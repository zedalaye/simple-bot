// Package dashboard décrit l'état du bot tel qu'il est exposé aux interfaces de
// supervision : le dashboard Telegram interactif et le relay de notifications
// mobile. Il ne contient aucune logique métier — uniquement des instantanés déjà
// mis en forme et l'interface que le bot implémente pour les produire.
//
// Ces types vivaient auparavant dans internal/telegram ; ils en ont été extraits
// pour qu'un second consommateur puisse les lire sans dépendre de Telegram.
package dashboard

import "time"

// StatusSnapshot est l'instantané de l'état général du bot.
type StatusSnapshot struct {
	Version      string // version du binaire (injectée par make release)
	Exchange     string
	Pair         string
	Price        string // déjà formaté selon la précision du marché
	RSI          string // formaté, ou "n/a"
	RSITimeframe string
	ActiveCycles int
	OpenCycles   int // cycles dont l'achat est rempli, en attente de vente
	OpenOrders   int // ordres encore ouverts sur l'exchange
	TotalProfit  float64
	AvgProfit    float64
	Quote        string
	Paused       bool
	UpdatedAt    time.Time
	Uptime       string // durée depuis le démarrage, ou "" si inconnu
	LastCheckAgo string // temps écoulé depuis le dernier price-check, ou "" si aucun
	ErrorMsg     string // dernière erreur récente, ou "" si aucune
	ErrorAgo     string // temps écoulé depuis cette erreur
}

// CycleView est une ligne de la vue des cycles actifs.
type CycleView struct {
	ID       int
	Status   string
	Amount   string
	BuyPrice string
	Target   string
	Age      string
}

// PnLSnapshot est l'instantané du résultat réalisé.
type PnLSnapshot struct {
	Completed   int
	TotalProfit float64
	AvgProfit   float64
	Quote       string
}

// BalanceLine est un solde d'actif.
type BalanceLine struct {
	Asset  string
	Amount string
	Locked string // montant bloqué dans des ordres ouverts, ou "" si aucun
	Value  string // valorisation en devise de cotation (total, dont bloqué), ou "" si inconnue
}

// BalanceSnapshot est l'instantané du portefeuille.
type BalanceSnapshot struct {
	Exchange string
	Lines    []BalanceLine
	Total    string // total valorisé, ou "" si rien de valorisable
}

// Source est la source de données et de contrôle fournie par le bot.
//
// Volontairement limitée à la lecture et à pause/reprise : rien de ce qui engage
// de l'argent (achat manuel) n'y figure, afin qu'un consommateur distant — le
// relay, joignable depuis internet — ne puisse pas déclencher d'ordre.
type Source interface {
	Status() (StatusSnapshot, error)
	Cycles() ([]CycleView, error)
	PnL() (PnLSnapshot, error)
	Balance() (BalanceSnapshot, error)
	Pause() error
	Resume() error
}
