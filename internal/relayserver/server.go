// Package relayserver implémente le relay que le bot alimente et que
// l'application mobile consulte.
//
// Il joue le rôle de boîte aux lettres entre deux moitiés qui ne peuvent pas se
// parler directement : le bot vit derrière un firewall domestique et n'accepte
// aucune connexion entrante, le téléphone n'est joignable que via un service de
// push. Le relay est le seul point que les deux atteignent.
//
// Il porte deux surfaces d'authentification distinctes : un jeton d'ingestion
// pour le bot, un jeton d'API pour l'application. Compromettre l'une ne donne
// pas l'autre.
package relayserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"bot/internal/logger"
	"bot/internal/relay/contract"
)

// defaultSilence : au-delà de ce délai sans snapshot, le bot est considéré comme
// muet. Assez large pour absorber quelques ticks manqués (le bot émet chaque
// minute par défaut) sans crier au loup à la moindre coupure réseau.
const defaultSilence = 5 * time.Minute

// watchdogInterval : fréquence de vérification du silence des instances.
const watchdogInterval = 30 * time.Second

// Config paramètre le relay.
type Config struct {
	Addr        string // adresse d'écoute, ex. « :9000 »
	IngestToken string // jeton présenté par le bot
	APIToken    string // jeton présenté par l'application mobile
	Silence     time.Duration
	Push        PushConfig
	// Assets sert l'application de supervision à la racine. Nil = API seule
	// (les tests n'ont pas besoin du front).
	Assets fs.FS
}

// Server expose les endpoints du relay.
type Server struct {
	cfg    Config
	store  *Store
	pusher *Pusher

	mu      sync.Mutex
	silent  map[string]bool // instances déjà signalées muettes (anti-répétition)
	nowFunc func() time.Time
}

func New(cfg Config, store *Store) *Server {
	if cfg.Silence <= 0 {
		cfg.Silence = defaultSilence
	}
	return &Server{
		cfg:     cfg,
		store:   store,
		pusher:  NewPusher(cfg.Push, store),
		silent:  make(map[string]bool),
		nowFunc: time.Now,
	}
}

// recordEvent enregistre un événement et le pousse vers les téléphones abonnés.
//
// Tous les événements passent par ici — ceux du bot comme ceux que le relay
// produit lui-même — pour qu'aucun ne puisse être stocké sans être notifié.
func (s *Server) recordEvent(e contract.Event) StoredEvent {
	stored := s.store.AppendEvent(e)
	// En arrière-plan : le bot attend la réponse de l'ingestion, il n'a pas à
	// patienter le temps que le service de push réponde.
	go s.pusher.Broadcast(e)
	return stored
}

// Handler construit le routeur du relay.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Surface bot : alimentée par internal/relay.
	mux.HandleFunc("POST /ingest/snapshot", s.ingest(s.handleSnapshot))
	mux.HandleFunc("POST /ingest/event", s.ingest(s.handleEvent))

	// Surface application mobile.
	mux.HandleFunc("GET /api/state", s.api(s.handleState))
	mux.HandleFunc("GET /api/events", s.api(s.handleEvents))
	mux.HandleFunc("POST /api/commands", s.api(s.handleCommand))
	mux.HandleFunc("GET /api/push/key", s.api(s.handlePushKey))
	mux.HandleFunc("POST /api/push/subscribe", s.api(s.handlePushSubscribe))
	mux.HandleFunc("POST /api/push/unsubscribe", s.api(s.handlePushUnsubscribe))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
	})

	// L'application est servie sans authentification : elle ne contient aucune
	// donnée, et réclame le jeton d'API au premier écran pour obtenir quoi que
	// ce soit. Les motifs ci-dessus étant plus spécifiques, ils gagnent.
	if s.cfg.Assets != nil {
		mux.Handle("GET /", http.FileServerFS(s.cfg.Assets))
	}

	return mux
}

// StartWatchdog surveille le silence des instances jusqu'à annulation de ctx.
//
// C'est le « dead man's switch » : la seule alerte que le bot ne peut pas
// émettre lui-même, puisqu'elle signale justement qu'il ne parle plus.
func (s *Server) StartWatchdog(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(watchdogInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkSilence()
			}
		}
	}()
}

func (s *Server) checkSilence() {
	for _, instance := range s.store.Instances() {
		_, receivedAt, ok := s.store.Snapshot(instance)
		if !ok {
			continue
		}

		silentFor := s.nowFunc().Sub(receivedAt)
		isSilent := silentFor > s.cfg.Silence

		s.mu.Lock()
		alreadyAlerted := s.silent[instance]
		s.silent[instance] = isSilent
		s.mu.Unlock()

		switch {
		case isSilent && !alreadyAlerted:
			s.recordEvent(contract.Event{
				Instance: instance,
				At:       s.nowFunc().UTC(),
				Kind:     contract.KindBotSilent,
				Level:    contract.LevelError,
				Title:    "Bot silencieux",
				Text:     "Aucun snapshot depuis " + silentFor.Round(time.Second).String(),
			})
			logger.Errorf("relay: instance %s muette depuis %v", instance, silentFor.Round(time.Second))

		case !isSilent && alreadyAlerted:
			s.recordEvent(contract.Event{
				Instance: instance,
				At:       s.nowFunc().UTC(),
				Kind:     contract.KindBotSilent,
				Level:    contract.LevelInfo,
				Title:    "Bot de nouveau joignable",
			})
			logger.Infof("relay: instance %s de nouveau joignable", instance)
		}
	}
}

// ===============================
// INGESTION (bot)
// ===============================

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	var snap contract.Snapshot
	if !decodeJSON(w, r, &snap) {
		return
	}
	if snap.Instance == "" {
		writeError(w, http.StatusBadRequest, "champ instance manquant")
		return
	}

	s.store.PutSnapshot(snap)

	// La réponse est le seul canal entrant vers le bot : on y glisse les
	// commandes en attente.
	writeJSON(w, http.StatusOK, contract.SnapshotReply{
		Commands: s.store.TakeCommands(snap.Instance),
	})
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	var e contract.Event
	if !decodeJSON(w, r, &e) {
		return
	}
	if e.Instance == "" {
		writeError(w, http.StatusBadRequest, "champ instance manquant")
		return
	}

	stored := s.recordEvent(e)
	writeJSON(w, http.StatusAccepted, map[string]int64{"seq": stored.Seq})
}

// ===============================
// LECTURE ET CONTRÔLE (application)
// ===============================

// InstanceView est l'état d'une instance tel que l'affiche l'application.
type InstanceView struct {
	Instance string `json:"instance"`
	// Online : le relay reçoit encore des snapshots. C'est une information que
	// le relay produit seul, le bot ne pouvant pas signaler sa propre absence.
	Online     bool               `json:"online"`
	SilentForS int64              `json:"silent_for_s"`
	Snapshot   *contract.Snapshot `json:"snapshot,omitempty"`
	Commands   []CommandState     `json:"commands,omitempty"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	instances := s.store.Instances()
	views := make([]InstanceView, 0, len(instances))

	for _, instance := range instances {
		snap, receivedAt, ok := s.store.Snapshot(instance)
		if !ok {
			continue
		}
		silentFor := s.nowFunc().Sub(receivedAt)
		views = append(views, InstanceView{
			Instance:   instance,
			Online:     silentFor <= s.cfg.Silence,
			SilentForS: int64(silentFor.Seconds()),
			Snapshot:   snap,
			Commands:   s.store.Commands(instance),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": views})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	instance := r.URL.Query().Get("instance")
	if instance == "" {
		writeError(w, http.StatusBadRequest, "paramètre instance manquant")
		return
	}

	var since int64
	if raw := r.URL.Query().Get("since"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "paramètre since invalide")
			return
		}
		since = v
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": s.store.Events(instance, since),
	})
}

// CommandRequest est le corps de POST /api/commands.
type CommandRequest struct {
	Instance string `json:"instance"`
	Action   string `json:"action"`
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	var req CommandRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Instance == "" {
		writeError(w, http.StatusBadRequest, "champ instance manquant")
		return
	}

	cmd, err := s.store.EnqueueCommand(req.Instance, req.Action)
	if err != nil {
		// Seules pause et resume existent : tout le reste est refusé ici, et le
		// serait de toute façon par le bot.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	logger.Infof("relay: commande %s mise en file pour %s (id=%s)", cmd.Action, req.Instance, cmd.ID)
	writeJSON(w, http.StatusAccepted, cmd)
}

// ===============================
// AUTHENTIFICATION
// ===============================

func (s *Server) ingest(next http.HandlerFunc) http.HandlerFunc {
	return s.authenticated(s.cfg.IngestToken, next)
}

func (s *Server) api(next http.HandlerFunc) http.HandlerFunc {
	return s.authenticated(s.cfg.APIToken, next)
}

// authenticated refuse la requête si le jeton attendu n'est pas présenté.
//
// Un jeton vide côté configuration ferme la surface au lieu de l'ouvrir : une
// variable d'environnement oubliée ne doit pas exposer le relay.
func (s *Server) authenticated(expected string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if expected == "" || !validBearer(r.Header.Get("Authorization"), expected) {
			writeError(w, http.StatusUnauthorized, "non autorisé")
			return
		}
		next(w, r)
	}
}

func validBearer(header, expected string) bool {
	if header == "" {
		return false
	}
	scheme, token, found := strings.Cut(header, " ")
	if !found || scheme != "Bearer" {
		return false
	}
	return subtleEqual(token, expected)
}

// subtleEqual compare en temps constant : le relay est exposé sur internet, une
// comparaison classique laisserait fuir la longueur du préfixe correct.
func subtleEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ===============================
// HELPERS HTTP
// ===============================

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "corps JSON illisible")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logger.Errorf("relay: réponse non écrite : %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
