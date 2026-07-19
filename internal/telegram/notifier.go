package telegram

import (
	"fmt"
	"strings"

	"bot/internal/notify"
)

// notifier rend un notify.Event en message Telegram.
//
// C'est ici que vit toute la mise en forme (emoji, renvois vers /status ou vers
// le bouton d'achat) : elle est propre au canal et n'a rien à faire dans la
// logique métier du bot.
type notifier struct {
	exchange string
}

// NewNotifier construit le canal Telegram. L'exchange est constant pour le
// process, il n'a donc pas à être porté par chaque événement.
//
// Le canal est inerte si TELEGRAM != "1" (voir SendMessage).
func NewNotifier(exchange string) notify.Notifier {
	return &notifier{exchange: exchange}
}

func (n *notifier) Notify(e notify.Event) error {
	return SendMessage(n.render(e))
}

func (n *notifier) render(e notify.Event) string {
	f := func(key string) string { return e.Fields[key] }

	switch e.Kind {
	case notify.KindBuyFilled:
		var b strings.Builder
		fmt.Fprintf(&b, "🌀 Cycle on %s [%s] UPDATE", n.exchange, f("cycle_id"))
		fmt.Fprintf(&b, "\n✅ Buy Order Filled: %s", f("order_id"))
		fmt.Fprintf(&b, "\n💰 Quantity: %s %s", f("quantity"), f("base"))
		fmt.Fprintf(&b, "\n📉 Buy Price: %s %s", f("price"), f("quote"))
		fmt.Fprintf(&b, "\n💲 Value: %s %s", f("value"), f("quote"))
		return b.String()

	case notify.KindSellFilled:
		var b strings.Builder
		fmt.Fprintf(&b, "🌀 Cycle on %s [%s] COMPLETE", n.exchange, f("cycle_id"))
		fmt.Fprintf(&b, "\n✅ Sell Order Filled: %s", f("order_id"))
		fmt.Fprintf(&b, "\n💰 Quantity: %s %s", f("quantity"), f("base"))
		fmt.Fprintf(&b, "\n📈 Sell Price: %s %s", f("price"), f("quote"))
		fmt.Fprintf(&b, "\n💲 Value: %s %s", f("value"), f("quote"))
		fmt.Fprintf(&b, "\n🤑 Profit: %s %s (%s%%)", f("profit"), f("quote"), f("profit_pct"))
		return b.String()

	case notify.KindPattern:
		var b strings.Builder
		fmt.Fprintf(&b, "🔔 [%s] Signal de retournement haussier — %s (%s)\n",
			n.exchange, f("pair"), f("timeframe"))
		b.WriteString(f("header"))
		if d := f("details"); d != "" {
			fmt.Fprintf(&b, "\n%s", d)
		}
		fmt.Fprintf(&b, "\nClôture : %s %s", f("close_price"), f("quote"))
		b.WriteString("\nCreux possible. /status ou bouton 🛒 Acheter pour agir.")
		return b.String()

	case notify.KindError:
		return fmt.Sprintf("⚠️ [%s] %s — ouvre /status\n%s", n.exchange, e.Title, e.Text)
	}

	// Type inconnu : on retombe sur le résumé neutre porté par l'événement.
	return fmt.Sprintf("[%s] %s\n%s", n.exchange, e.Title, e.Text)
}
