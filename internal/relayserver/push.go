package relayserver

import (
	"encoding/json"
	"net/http"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"

	"bot/internal/logger"
	"bot/internal/relay/contract"
)

// Web Push, en quatre temps :
//
//  1. La PWA s'abonne auprès du service de push de son navigateur (FCM sur
//     Android) et en rapporte un endpoint et deux clés.
//  2. Le relay signe chaque envoi avec sa paire VAPID : c'est ce qui l'identifie
//     auprès du service de push, et ce qui empêche un tiers ayant intercepté un
//     endpoint de s'en servir.
//  3. La charge utile est chiffrée pour le navigateur avec les clés de
//     l'abonnement. Le service de push relaie un contenu qu'il ne peut pas lire.
//  4. Le service de push réveille le service worker, qui affiche la notification.
//
// C'est le service de push, et non le relay, qui gère la file d'attente quand le
// téléphone est éteint ou hors ligne.

// pushTTL : durée pendant laquelle le service de push conserve un message non
// remis. Au-delà d'une journée, une alerte de trading n'a plus d'intérêt.
const pushTTL = 24 * 60 * 60

// PushConfig porte la paire VAPID du relay.
//
// Elle doit rester stable : la clé publique est scellée dans les abonnements des
// navigateurs. En changer invalide tous les abonnements existants.
type PushConfig struct {
	PublicKey  string
	PrivateKey string
	// Subscriber : contact (mailto:…) que le service de push peut utiliser en
	// cas de problème. Exigé par la spécification VAPID.
	Subscriber string
}

// Enabled indique si le Web Push est configuré.
func (c PushConfig) Enabled() bool { return c.PublicKey != "" && c.PrivateKey != "" }

// Pusher diffuse les événements vers les abonnements enregistrés.
type Pusher struct {
	cfg   PushConfig
	store *Store
}

func NewPusher(cfg PushConfig, store *Store) *Pusher {
	return &Pusher{cfg: cfg, store: store}
}

// pushPayload est ce que reçoit le service worker. Volontairement compact : les
// services de push limitent la taille des charges utiles (~4 Ko).
type pushPayload struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Kind     string `json:"kind"`
	Level    string `json:"level"`
	Instance string `json:"instance"`
	// Tag regroupe les notifications : une nouvelle alerte d'erreur remplace la
	// précédente au lieu d'empiler des doublons dans le tiroir.
	Tag string `json:"tag"`
}

// Broadcast envoie un événement à tous les abonnements.
//
// Appelée en arrière-plan : l'ingestion ne doit pas attendre le service de push,
// puisque c'est le bot qui patiente à l'autre bout de la requête.
func (p *Pusher) Broadcast(e contract.Event) {
	if !p.cfg.Enabled() {
		return
	}

	subs := p.store.Subscriptions()
	if len(subs) == 0 {
		return
	}

	body, err := json.Marshal(pushPayload{
		Title:    e.Title,
		Body:     e.Text,
		Kind:     e.Kind,
		Level:    e.Level,
		Instance: e.Instance,
		Tag:      e.Instance + ":" + e.Kind,
	})
	if err != nil {
		logger.Errorf("push: charge utile non sérialisable : %v", err)
		return
	}

	for _, sub := range subs {
		p.send(body, sub, urgencyFor(e.Level))
	}
}

func (p *Pusher) send(body []byte, sub Subscription, urgency webpush.Urgency) {
	resp, err := webpush.SendNotification(body, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{Auth: sub.Keys.Auth, P256dh: sub.Keys.P256dh},
	}, &webpush.Options{
		Subscriber:      p.cfg.Subscriber,
		VAPIDPublicKey:  p.cfg.PublicKey,
		VAPIDPrivateKey: p.cfg.PrivateKey,
		TTL:             pushTTL,
		Urgency:         urgency,
	})
	if err != nil {
		logger.Errorf("push: envoi impossible vers %s : %v", shortEndpoint(sub.Endpoint), err)
		return
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		// Le navigateur a désinstallé la PWA ou révoqué l'abonnement : le garder
		// ne ferait qu'échouer indéfiniment.
		p.store.RemoveSubscription(sub.Endpoint)
		logger.Infof("push: abonnement périmé oublié (%s)", shortEndpoint(sub.Endpoint))

	case resp.StatusCode >= 300:
		logger.Errorf("push: %s a répondu %s", shortEndpoint(sub.Endpoint), resp.Status)
	}
}

// urgencyFor : une erreur mérite de réveiller un téléphone en économie de
// batterie, une notification d'achat rempli peut attendre.
func urgencyFor(level string) webpush.Urgency {
	if level == contract.LevelError {
		return webpush.UrgencyHigh
	}
	return webpush.UrgencyNormal
}

// shortEndpoint tronque l'endpoint pour les logs : il est long et vaut jeton
// d'envoi, inutile de l'écrire en entier dans un fichier.
func shortEndpoint(endpoint string) string {
	if i := strings.LastIndex(endpoint, "/"); i >= 0 && i+9 < len(endpoint) {
		return "…" + endpoint[i+1:i+9]
	}
	return "…"
}

// ===============================
// ENDPOINTS
// ===============================

func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Push.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "web push non configuré sur ce relay")
		return
	}
	// La clé publique n'est pas un secret : le navigateur en a besoin pour
	// s'abonner, et elle sert justement à vérifier l'identité de l'émetteur.
	writeJSON(w, http.StatusOK, map[string]string{"key": s.cfg.Push.PublicKey})
}

func (s *Server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var sub Subscription
	if !decodeJSON(w, r, &sub) {
		return
	}
	if sub.Endpoint == "" || sub.Keys.Auth == "" || sub.Keys.P256dh == "" {
		writeError(w, http.StatusBadRequest, "abonnement incomplet")
		return
	}

	s.store.AddSubscription(sub)
	logger.Infof("push: abonnement enregistré (%s)", shortEndpoint(sub.Endpoint))
	writeJSON(w, http.StatusCreated, map[string]string{"status": "abonné"})
}

func (s *Server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "champ endpoint manquant")
		return
	}

	s.store.RemoveSubscription(req.Endpoint)
	writeJSON(w, http.StatusOK, map[string]string{"status": "désabonné"})
}
