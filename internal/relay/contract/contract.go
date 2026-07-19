// Package contract décrit le format JSON échangé entre le bot et le relay de
// notifications mobile.
//
// Il est importé des deux côtés — le client dans internal/relay, le serveur dans
// internal/relayserver — précisément pour qu'ils ne puissent pas diverger :
// ajouter un champ d'un seul côté ne compile pas.
//
// Deux règles à respecter :
//
//   - Ce paquet ne dépend que de la bibliothèque standard. Le binaire du relay ne
//     doit tirer ni ccxt ni SQLite, sous peine de passer de quelques Mo à plus de
//     cent. Un test verrouille cette contrainte.
//   - Les noms de champs JSON sont une interface publique : les changer impose de
//     redéployer le bot ET le relay.
package contract

import "time"

// Event est une notification à diffuser : corps de POST /ingest/event.
type Event struct {
	Instance string            `json:"instance"`
	At       time.Time         `json:"at"`
	Kind     string            `json:"kind"`
	Level    string            `json:"level"`
	Title    string            `json:"title"`
	Text     string            `json:"text,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`
}

// Niveaux de gravité d'un Event.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// KindBotSilent est émis par le relay lui-même — et non par le bot — lorsque les
// snapshots cessent d'arriver. C'est le « dead man's switch » : la seule alerte
// que le bot ne peut structurellement pas envoyer, puisqu'il est tombé.
const KindBotSilent = "bot_silent"

// Snapshot est l'état courant du bot : corps de POST /ingest/snapshot.
// Il fait aussi office de battement de cœur.
type Snapshot struct {
	Instance string    `json:"instance"`
	Version  string    `json:"version"`
	At       time.Time `json:"at"`
	Exchange string    `json:"exchange"`
	Pair     string    `json:"pair"`
	Quote    string    `json:"quote"`

	Paused bool `json:"paused"`
	// Durées en secondes ; 0 = inconnu. C'est LastCheckAgoS qui permet de juger
	// si le bot travaille encore, au-delà du simple fait qu'il émette.
	UptimeS       int64 `json:"uptime_s"`
	LastCheckAgoS int64 `json:"last_check_ago_s"`

	Price        string `json:"price"`
	RSI          string `json:"rsi,omitempty"`
	RSITimeframe string `json:"rsi_timeframe,omitempty"`

	ActiveCycles int `json:"active_cycles"`
	OpenCycles   int `json:"open_cycles"`
	OpenOrders   int `json:"open_orders"`

	// Error porte la dernière erreur récente, absente si le bot va bien.
	Error *ErrorInfo `json:"error,omitempty"`
	// Portfolio n'accompagne que les snapshots « lents » : sa production coûte un
	// appel à l'exchange, on ne la demande donc pas à chaque tick.
	Portfolio *Portfolio `json:"portfolio,omitempty"`
	// Acks acquitte les commandes reçues lors des snapshots précédents.
	Acks []Ack `json:"acks,omitempty"`
}

// ErrorInfo résume la dernière erreur rencontrée par le bot.
type ErrorInfo struct {
	Message string `json:"message"`
	AgoS    int64  `json:"ago_s"`
}

// Portfolio valorise le portefeuille. Les montants sont formatés sans devise,
// celle-ci étant portée une fois par Quote.
type Portfolio struct {
	Total  string        `json:"total"`
	Locked string        `json:"locked,omitempty"`
	Quote  string        `json:"quote"`
	Lines  []BalanceLine `json:"lines,omitempty"`
}

// BalanceLine est le solde d'un actif.
type BalanceLine struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
	Locked string `json:"locked,omitempty"`
	Value  string `json:"value,omitempty"`
}

// SnapshotReply est la réponse du relay au snapshot.
//
// C'est le seul canal entrant vers le bot : il vit derrière un firewall et
// n'accepte aucune connexion. On réutilise donc l'aller-retour du snapshot pour
// lui transmettre les commandes en attente, au prix d'une latence d'un tick.
type SnapshotReply struct {
	Commands []Command `json:"commands"`
}

// Command est une action demandée au bot.
//
// ID doit être unique et stable : le bot s'en sert pour ne pas rejouer une
// commande dont l'acquittement se serait perdu.
type Command struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

// Actions acceptées par le bot. Toute autre valeur est refusée.
//
// Cette liste est délibérément limitée à ce qui n'engage pas d'argent : le bot
// n'a aucun code pour exécuter un achat sur commande distante, si bien qu'un
// relay compromis ne peut pas en déclencher un.
const (
	ActionPause  = "pause"
	ActionResume = "resume"
)

// Ack acquitte l'exécution d'une commande, joint au snapshot suivant.
type Ack struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}
