package relay

import "time"

// Contrat JSON échangé avec le relay. Les noms de champs font partie de
// l'interface publique du bot : les modifier impose de mettre le relay à jour.

// eventPayload est le corps de POST /ingest/event.
type eventPayload struct {
	Instance string            `json:"instance"`
	At       time.Time         `json:"at"`
	Kind     string            `json:"kind"`
	Level    string            `json:"level"`
	Title    string            `json:"title"`
	Text     string            `json:"text,omitempty"`
	Fields   map[string]string `json:"fields,omitempty"`
}

// snapshotPayload est le corps de POST /ingest/snapshot.
type snapshotPayload struct {
	Instance string    `json:"instance"`
	Version  string    `json:"version"`
	At       time.Time `json:"at"`
	Exchange string    `json:"exchange"`
	Pair     string    `json:"pair"`
	Quote    string    `json:"quote"`

	Paused bool `json:"paused"`
	// Durées en secondes ; 0 = inconnu. C'est last_check_ago_s qui permet au
	// relay de juger si le bot est encore vivant.
	UptimeS       int64 `json:"uptime_s"`
	LastCheckAgoS int64 `json:"last_check_ago_s"`

	Price        string `json:"price"`
	RSI          string `json:"rsi,omitempty"`
	RSITimeframe string `json:"rsi_timeframe,omitempty"`

	ActiveCycles int `json:"active_cycles"`
	OpenCycles   int `json:"open_cycles"`
	OpenOrders   int `json:"open_orders"`

	// Error porte la dernière erreur récente, absente si le bot va bien.
	Error *errorPayload `json:"error,omitempty"`
	// Portfolio n'est présent que sur les ticks lents (voir BalanceInterval).
	Portfolio *portfolioPayload `json:"portfolio,omitempty"`
	// Acks acquitte les commandes reçues lors des snapshots précédents.
	Acks []ack `json:"acks,omitempty"`
}

type errorPayload struct {
	Message string `json:"message"`
	AgoS    int64  `json:"ago_s"`
}

// portfolioPayload : montants formatés sans devise, celle-ci étant portée par Quote.
type portfolioPayload struct {
	Total  string        `json:"total"`
	Locked string        `json:"locked,omitempty"`
	Quote  string        `json:"quote"`
	Lines  []linePayload `json:"lines,omitempty"`
}

type linePayload struct {
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
	Locked string `json:"locked,omitempty"`
	Value  string `json:"value,omitempty"`
}

// snapshotReply est la réponse du relay au snapshot : elle transporte les
// commandes en attente, seul canal entrant vers le bot.
type snapshotReply struct {
	Commands []command `json:"commands"`
}

// command : seules les actions « pause » et « resume » sont honorées.
type command struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

// ack acquitte l'exécution d'une commande, joint au snapshot suivant.
type ack struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}
