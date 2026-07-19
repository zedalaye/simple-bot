package relayserver

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"time"

	"bot/internal/relay/contract"
)

// eventHistory : nombre d'événements conservés par instance. Au-delà, les plus
// anciens sont oubliés — l'application mobile n'affiche qu'un historique récent.
const eventHistory = 200

// commandTTL : au-delà, une commande jamais acquittée est abandonnée. Sans cela
// une commande émise pendant que le bot était éteint serait rejouée à son retour,
// des heures plus tard, alors qu'elle n'a plus de sens.
const commandTTL = 15 * time.Minute

// StoredEvent est un événement reçu, augmenté de quoi le paginer.
type StoredEvent struct {
	Seq        int64          `json:"seq"`
	ReceivedAt time.Time      `json:"received_at"`
	Event      contract.Event `json:"event"`
}

// CommandState suit le cycle de vie d'une commande, de la demande faite depuis
// l'application mobile jusqu'à son acquittement par le bot.
type CommandState struct {
	ID          string     `json:"id"`
	Action      string     `json:"action"`
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	AckedAt     *time.Time `json:"acked_at,omitempty"`
	OK          *bool      `json:"ok,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// Pending indique que la commande n'a pas encore été acquittée.
func (c *CommandState) Pending() bool { return c.AckedAt == nil }

// Subscription est un abonnement Web Push, tel que le navigateur le produit.
// Les clés servent au chiffrement de bout en bout : le service de push relaie
// une charge utile qu'il ne peut pas lire.
type Subscription struct {
	Endpoint  string    `json:"endpoint"`
	Keys      PushKeys  `json:"keys"`
	CreatedAt time.Time `json:"created_at"`
}

// PushKeys porte les clés publiées par le navigateur à l'abonnement.
type PushKeys struct {
	Auth   string `json:"auth"`
	P256dh string `json:"p256dh"`
}

type instanceState struct {
	snapshot   *contract.Snapshot
	receivedAt time.Time

	events []StoredEvent
	seq    int64

	commands []*CommandState
}

// Store conserve l'état des instances en mémoire.
//
// Volontairement non persistant pour l'instant : tout ce qu'il contient est soit
// rafraîchi au prochain snapshot (l'état), soit un historique d'agrément (les
// événements). Un redémarrage du relay coûte l'historique, pas la supervision.
// Y adosser Redis ou SQLite reste possible derrière la même API.
type Store struct {
	mu        sync.Mutex
	instances map[string]*instanceState
	// Les abonnements push sont globaux : on s'abonne depuis un téléphone, pas
	// pour une instance en particulier.
	subscriptions map[string]Subscription // indexés par endpoint
}

func NewStore() *Store {
	return &Store{
		instances:     make(map[string]*instanceState),
		subscriptions: make(map[string]Subscription),
	}
}

// AddSubscription enregistre un abonnement push. L'endpoint sert de clé : se
// réabonner depuis le même navigateur remplace l'entrée au lieu de la dupliquer.
func (s *Store) AddSubscription(sub Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now()
	}
	s.subscriptions[sub.Endpoint] = sub
}

// RemoveSubscription oublie un abonnement, sur désinscription volontaire ou
// parce que le service de push l'a déclaré périmé.
func (s *Store) RemoveSubscription(endpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscriptions, endpoint)
}

// Subscriptions retourne les abonnements actifs.
func (s *Store) Subscriptions() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Subscription, 0, len(s.subscriptions))
	for _, sub := range s.subscriptions {
		out = append(out, sub)
	}
	return out
}

func (s *Store) get(instance string) *instanceState {
	st, ok := s.instances[instance]
	if !ok {
		st = &instanceState{}
		s.instances[instance] = st
	}
	return st
}

// PutSnapshot enregistre l'état d'une instance et enregistre les acquittements
// qui l'accompagnent.
func (s *Store) PutSnapshot(snap contract.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.get(snap.Instance)
	st.snapshot = &snap
	st.receivedAt = time.Now()

	for _, ack := range snap.Acks {
		for _, cmd := range st.commands {
			if cmd.ID != ack.ID || cmd.AckedAt != nil {
				continue
			}
			now := time.Now()
			ok := ack.OK
			cmd.AckedAt = &now
			cmd.OK = &ok
			cmd.Error = ack.Error
		}
	}
}

// Snapshot retourne le dernier état connu d'une instance et la date de réception.
func (s *Store) Snapshot(instance string) (*contract.Snapshot, time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.instances[instance]
	if !ok || st.snapshot == nil {
		return nil, time.Time{}, false
	}
	return st.snapshot, st.receivedAt, true
}

// Instances liste les instances connues.
func (s *Store) Instances() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	names := make([]string, 0, len(s.instances))
	for name := range s.instances {
		names = append(names, name)
	}
	return names
}

// AppendEvent enregistre un événement et retourne sa forme stockée.
func (s *Store) AppendEvent(e contract.Event) StoredEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.get(e.Instance)
	st.seq++
	stored := StoredEvent{Seq: st.seq, ReceivedAt: time.Now(), Event: e}

	st.events = append(st.events, stored)
	if len(st.events) > eventHistory {
		st.events = st.events[len(st.events)-eventHistory:]
	}
	return stored
}

// Events retourne les événements d'une instance dont le numéro de séquence est
// strictement supérieur à since (0 pour tout l'historique conservé).
func (s *Store) Events(instance string, since int64) []StoredEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.instances[instance]
	if !ok {
		return nil
	}

	out := make([]StoredEvent, 0, len(st.events))
	for _, e := range st.events {
		if e.Seq > since {
			out = append(out, e)
		}
	}
	return out
}

// EnqueueCommand met une commande en file pour une instance. L'action est
// vérifiée ici en plus de l'être par le bot : autant refuser au plus tôt.
func (s *Store) EnqueueCommand(instance, action string) (*CommandState, error) {
	if action != contract.ActionPause && action != contract.ActionResume {
		return nil, fmt.Errorf("action non autorisée : %q", action)
	}

	id, err := newCommandID()
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cmd := &CommandState{ID: id, Action: action, CreatedAt: time.Now()}
	st := s.get(instance)
	st.commands = append(st.commands, cmd)
	return cmd, nil
}

// TakeCommands retourne les commandes à transmettre au bot.
//
// Une commande est renvoyée à *chaque* snapshot tant qu'elle n'est pas
// acquittée : c'est ce qui la rend robuste à la perte d'une réponse HTTP. Le bot
// sait ne pas la rejouer grâce à son identifiant.
func (s *Store) TakeCommands(instance string) []contract.Command {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.instances[instance]
	if !ok {
		return nil
	}

	now := time.Now()
	var out []contract.Command
	for _, cmd := range st.commands {
		if !cmd.Pending() {
			continue
		}
		if now.Sub(cmd.CreatedAt) > commandTTL {
			// Abandon : on l'acquitte nous-mêmes en échec pour que l'application
			// mobile cesse de l'afficher comme « en attente ».
			failed := false
			cmd.AckedAt = &now
			cmd.OK = &failed
			cmd.Error = "expirée : le bot ne l'a pas réclamée à temps"
			continue
		}
		if cmd.DeliveredAt == nil {
			delivered := now
			cmd.DeliveredAt = &delivered
		}
		out = append(out, contract.Command{ID: cmd.ID, Action: cmd.Action})
	}
	return out
}

// Commands retourne l'état des commandes d'une instance, les plus récentes en
// premier.
func (s *Store) Commands(instance string) []CommandState {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.instances[instance]
	if !ok {
		return nil
	}

	out := make([]CommandState, 0, len(st.commands))
	for _, cmd := range slices.Backward(st.commands) {
		out = append(out, *cmd)
	}
	return out
}

func newCommandID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("identifiant de commande : %w", err)
	}
	return "c_" + hex.EncodeToString(b[:]), nil
}
