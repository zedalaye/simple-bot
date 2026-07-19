// Package relay pousse l'état du bot et ses notifications vers un relay HTTP
// auto-hébergé, que consulte une application mobile.
//
// Le bot tourne derrière un firewall domestique et n'est joignable d'aucune
// façon depuis l'extérieur : tout est donc à son initiative. Deux flux sortants
// suffisent :
//
//   - POST /ingest/event    — une notification à diffuser (achat, vente, erreur…)
//   - POST /ingest/snapshot — l'état courant, à intervalle régulier
//
// Le second sert aussi de battement de cœur : si le relay cesse de recevoir des
// snapshots, il sait que le bot est tombé et peut alerter — ce qu'aucune
// notification sortante ne pourrait signaler.
//
// Enfin, la *réponse* au snapshot transporte les commandes en attente. C'est ce
// qui permet de piloter le bot depuis l'extérieur sans ouvrir de port : on
// réutilise un aller-retour qui existe déjà, au prix d'une latence d'un tick.
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"bot/internal/dashboard"
	"bot/internal/logger"
	"bot/internal/notify"
	"bot/internal/relay/contract"
)

// eventAttempts : nombre d'envois tentés pour une notification. Contrairement au
// snapshot (rejoué au tick suivant), un événement manqué est perdu.
const eventAttempts = 3

// executedHistory : nombre de commandes déjà exécutées dont on garde la trace
// pour l'idempotence. Largement au-dessus de ce qu'une file peut contenir entre
// deux ticks.
const executedHistory = 256

// Config paramètre le client. URL vide = relay désactivé.
type Config struct {
	URL             string        // base du relay, ex. https://relay.example.com
	Token           string        // jeton porté en Bearer sur chaque requête
	Instance        string        // identifiant de l'instance (ex. « mexc »)
	Interval        time.Duration // cadence des snapshots
	BalanceInterval time.Duration // cadence de la valorisation du portefeuille
}

// Enabled indique si le relay est configuré.
func (c Config) Enabled() bool { return strings.TrimSpace(c.URL) != "" }

// Client diffuse vers le relay. Il implémente notify.Notifier pour les
// événements, et Start() anime la boucle de snapshots.
type Client struct {
	cfg  Config
	src  dashboard.Source
	http *http.Client
	// retryBackoff : attente de base entre deux tentatives d'envoi d'événement.
	// Réglable pour que les tests n'aient pas à patienter.
	retryBackoff time.Duration

	mu       sync.Mutex
	acks     []contract.Ack  // acquittements à joindre au prochain snapshot
	executed map[string]bool // commandes déjà exécutées (idempotence)
	order    []string        // ordre d'insertion, pour purger executed
}

// New construit le client. src fournit l'état et le contrôle pause/reprise ;
// il ne permet volontairement aucune opération engageant de l'argent.
func New(cfg Config, src dashboard.Source) *Client {
	return &Client{
		cfg:          cfg,
		src:          src,
		http:         &http.Client{Timeout: 10 * time.Second},
		retryBackoff: time.Second,
		executed:     make(map[string]bool),
	}
}

// ===============================
// ÉVÉNEMENTS
// ===============================

func (c *Client) Notify(e notify.Event) error {
	at := e.At
	if at.IsZero() {
		at = time.Now()
	}

	payload := contract.Event{
		Instance: c.cfg.Instance,
		At:       at.UTC(),
		Kind:     string(e.Kind),
		Level:    string(e.Level),
		Title:    e.Title,
		Text:     e.Text,
		Fields:   e.Fields,
	}

	var err error
	for attempt := 1; attempt <= eventAttempts; attempt++ {
		if err = c.post("/ingest/event", payload, nil); err == nil {
			return nil
		}
		if attempt < eventAttempts {
			time.Sleep(time.Duration(attempt) * c.retryBackoff)
		}
	}
	return fmt.Errorf("relay: événement non diffusé après %d tentatives : %w", eventAttempts, err)
}

// ===============================
// SNAPSHOTS
// ===============================

// Start anime la boucle de snapshots jusqu'à l'annulation de ctx. Un premier
// snapshot part immédiatement pour que l'application mobile ait un état sans
// attendre le premier tick.
func (c *Client) Start(ctx context.Context) {
	go func() {
		var lastBalance time.Time

		tick := func() {
			// La valorisation du portefeuille interroge l'exchange : on l'espace
			// davantage que le reste du snapshot, qui vient surtout de la base.
			withBalance := time.Since(lastBalance) >= c.cfg.BalanceInterval
			if c.sendSnapshot(withBalance) && withBalance {
				lastBalance = time.Now()
			}
		}

		tick()

		ticker := time.NewTicker(c.cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tick()
			}
		}
	}()
}

// sendSnapshot construit et envoie un snapshot, puis exécute les commandes
// reçues en réponse. Retourne false si l'envoi a échoué.
func (c *Client) sendSnapshot(withBalance bool) bool {
	status, err := c.src.Status()
	if err != nil {
		logger.Errorf("relay: état indisponible, snapshot ignoré : %v", err)
		return false
	}

	payload := contract.Snapshot{
		Instance:      c.cfg.Instance,
		Version:       status.Version,
		At:            time.Now().UTC(),
		Exchange:      status.Exchange,
		Pair:          status.Pair,
		Quote:         status.Quote,
		Paused:        status.Paused,
		UptimeS:       int64(status.Uptime.Seconds()),
		LastCheckAgoS: int64(status.LastCheckAgo.Seconds()),
		Price:         status.Price,
		RSI:           status.RSI,
		RSITimeframe:  status.RSITimeframe,
		ActiveCycles:  status.ActiveCycles,
		OpenCycles:    status.OpenCycles,
		OpenOrders:    status.OpenOrders,
		Acks:          c.takeAcks(),
	}

	if status.ErrorMsg != "" {
		payload.Error = &contract.ErrorInfo{
			Message: status.ErrorMsg,
			AgoS:    int64(status.ErrorAgo.Seconds()),
		}
	}

	if withBalance {
		if bal, err := c.src.Balance(); err != nil {
			// Non bloquant : le reste du snapshot reste utile, et la valorisation
			// repartira au prochain tick lent.
			logger.Errorf("relay: valorisation du portefeuille indisponible : %v", err)
		} else {
			payload.Portfolio = buildPortfolio(bal)
		}
	}

	var reply contract.SnapshotReply
	if err := c.post("/ingest/snapshot", payload, &reply); err != nil {
		// Les acks n'ont pas été transmis : on les remet en file pour le prochain tick.
		c.restoreAcks(payload.Acks)
		logger.Errorf("relay: snapshot non transmis : %v", err)
		return false
	}

	c.runCommands(reply.Commands)
	return true
}

func buildPortfolio(bal dashboard.BalanceSnapshot) *contract.Portfolio {
	p := &contract.Portfolio{
		Total:  bal.Total,
		Locked: bal.Locked,
		Quote:  bal.Quote,
	}
	for _, l := range bal.Lines {
		p.Lines = append(p.Lines, contract.BalanceLine{
			Asset:  l.Asset,
			Amount: l.Amount,
			Locked: l.Locked,
			Value:  l.Value,
		})
	}
	return p
}

// ===============================
// COMMANDES
// ===============================

// runCommands exécute les commandes reçues et prépare leurs acquittements.
//
// Seules pause et resume sont acceptées. Cette liste blanche est la garantie
// qu'un relay compromis ne peut pas déclencher d'opération engageant de
// l'argent : le bot n'a tout simplement aucun code pour l'exécuter.
func (c *Client) runCommands(cmds []contract.Command) {
	for _, cmd := range cmds {
		if cmd.ID == "" {
			logger.Errorf("relay: commande sans identifiant ignorée (action=%q)", cmd.Action)
			continue
		}

		// Idempotence : un ack perdu fait renvoyer la commande au tick suivant.
		// On la réacquitte sans la rejouer.
		if c.alreadyExecuted(cmd.ID) {
			c.addAck(contract.Ack{ID: cmd.ID, OK: true})
			continue
		}

		var err error
		switch cmd.Action {
		case contract.ActionPause:
			err = c.src.Pause()
		case contract.ActionResume:
			err = c.src.Resume()
		default:
			err = fmt.Errorf("action non autorisée : %q", cmd.Action)
			logger.Errorf("relay: commande refusée (id=%s, action=%q)", cmd.ID, cmd.Action)
		}

		c.markExecuted(cmd.ID)

		a := contract.Ack{ID: cmd.ID, OK: err == nil}
		if err != nil {
			a.Error = err.Error()
		} else {
			logger.Infof("relay: commande %s exécutée (id=%s)", cmd.Action, cmd.ID)
		}
		c.addAck(a)
	}
}

func (c *Client) alreadyExecuted(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.executed[id]
}

func (c *Client) markExecuted(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.executed[id] {
		return
	}
	c.executed[id] = true
	c.order = append(c.order, id)

	if len(c.order) > executedHistory {
		delete(c.executed, c.order[0])
		c.order = c.order[1:]
	}
}

func (c *Client) addAck(a contract.Ack) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acks = append(c.acks, a)
}

// takeAcks retire et retourne les acquittements en attente.
func (c *Client) takeAcks() []contract.Ack {
	c.mu.Lock()
	defer c.mu.Unlock()
	acks := c.acks
	c.acks = nil
	return acks
}

// restoreAcks remet en file des acquittements dont l'envoi a échoué.
func (c *Client) restoreAcks(acks []contract.Ack) {
	if len(acks) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acks = append(acks, c.acks...)
}

// ===============================
// TRANSPORT
// ===============================

func (c *Client) post(path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("relay: sérialisation impossible : %w", err)
	}

	url := strings.TrimSuffix(c.cfg.URL, "/") + path
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("relay: requête invalide : %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("relay: %s injoignable : %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Le corps peut porter un message d'erreur utile, mais on le borne.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("relay: %s a répondu %s (%s)", path, resp.Status, strings.TrimSpace(string(detail)))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("relay: réponse de %s illisible : %w", path, err)
	}
	return nil
}
