// Package notify définit un événement de notification indépendant du canal de
// diffusion, et l'interface que chaque canal implémente.
//
// Auparavant le bot construisait directement des messages Telegram (emoji,
// renvois vers /status) au cœur de la logique métier. En passant par un Event
// structuré, la mise en forme redescend dans chaque canal : Telegram rend du
// texte, le relay mobile sérialise du JSON que la SPA peut filtrer et afficher.
package notify

import (
	"errors"
	"time"
)

// Level classe la gravité d'un événement (repris tel quel par la SPA).
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Kind identifie la nature d'un événement. La valeur est reprise telle quelle
// dans le JSON envoyé au relay : ne pas la modifier sans mettre le relay à jour.
type Kind string

const (
	KindBuyFilled  Kind = "buy_filled"
	KindSellFilled Kind = "sell_filled"
	KindPattern    Kind = "pattern"
	KindError      Kind = "error"
)

// Event est une notification prête à diffuser.
//
// Title et Text portent un résumé lisible et neutre (utilisable tel quel par
// n'importe quel canal) ; Fields porte les mêmes informations sous forme
// structurée, pour les canaux capables d'un rendu riche.
type Event struct {
	Kind   Kind
	Level  Level
	Title  string
	Text   string
	Fields map[string]string
	At     time.Time
}

// Notifier diffuse un événement sur un canal donné.
type Notifier interface {
	Notify(Event) error
}

// Multi diffuse sur plusieurs canaux à la fois. Un canal en échec n'empêche pas
// les autres de recevoir l'événement : c'est ce qui permet de faire tourner
// Telegram et le relay en parallèle pendant la migration.
type Multi []Notifier

func (m Multi) Notify(e Event) error {
	var errs []error
	for _, n := range m {
		if n == nil {
			continue
		}
		if err := n.Notify(e); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Nop est un Notifier qui ne fait rien (aucun canal configuré).
func Nop() Notifier { return nop{} }

type nop struct{}

func (nop) Notify(Event) error { return nil }
