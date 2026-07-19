package telegram

import (
	"testing"

	"bot/internal/notify"
)

// Les messages attendus ci-dessous sont ceux produits par le bot avant
// l'extraction de notify.Event (formatage alors codé en dur dans internal/bot).
// Ils garantissent que le passage par l'Event structuré n'a rien changé à ce que
// reçoit l'utilisateur, condition pour faire tourner Telegram et le relay en
// parallèle pendant la migration.
func TestNotifierRenderPreserveLegacyMessages(t *testing.T) {
	n := &notifier{exchange: "mexc"}

	tests := []struct {
		name  string
		event notify.Event
		want  string
	}{
		{
			name: "achat rempli",
			event: notify.Event{
				Kind: notify.KindBuyFilled,
				Fields: map[string]string{
					"cycle_id": "42",
					"order_id": "C02__1",
					"quantity": "0.00017",
					"base":     "BTC",
					"price":    "58234.10",
					"quote":    "USDC",
					"value":    "9.90",
				},
			},
			want: "🌀 Cycle on mexc [42] UPDATE" +
				"\n✅ Buy Order Filled: C02__1" +
				"\n💰 Quantity: 0.00017 BTC" +
				"\n📉 Buy Price: 58234.10 USDC" +
				"\n💲 Value: 9.90 USDC",
		},
		{
			name: "vente remplie",
			event: notify.Event{
				Kind: notify.KindSellFilled,
				Fields: map[string]string{
					"cycle_id":   "42",
					"order_id":   "C02__2",
					"quantity":   "0.00017",
					"base":       "BTC",
					"price":      "59500.00",
					"quote":      "USDC",
					"value":      "10.11",
					"profit":     "0.21",
					"profit_pct": "+2.1",
				},
			},
			want: "🌀 Cycle on mexc [42] COMPLETE" +
				"\n✅ Sell Order Filled: C02__2" +
				"\n💰 Quantity: 0.00017 BTC" +
				"\n📈 Sell Price: 59500.00 USDC" +
				"\n💲 Value: 10.11 USDC" +
				"\n🤑 Profit: 0.21 USDC (+2.1%)",
		},
		{
			name: "pattern avec confirmations",
			event: notify.Event{
				Kind: notify.KindPattern,
				Fields: map[string]string{
					"pair":        "BTC/USDC",
					"timeframe":   "1h",
					"header":      "Marteau  ·  conviction forte",
					"details":     "RSI(1h) 28 (survendu)  ·  volume confirmé ✅",
					"close_price": "58234.10",
					"quote":       "USDC",
				},
			},
			want: "🔔 [mexc] Signal de retournement haussier — BTC/USDC (1h)\n" +
				"Marteau  ·  conviction forte" +
				"\nRSI(1h) 28 (survendu)  ·  volume confirmé ✅" +
				"\nClôture : 58234.10 USDC" +
				"\nCreux possible. /status ou bouton 🛒 Acheter pour agir.",
		},
		{
			// Sans confirmation, l'ancien code n'insérait aucune ligne de détail.
			name: "pattern sans confirmation",
			event: notify.Event{
				Kind: notify.KindPattern,
				Fields: map[string]string{
					"pair":        "BTC/USDC",
					"timeframe":   "1h",
					"header":      "Étoile du matin",
					"details":     "",
					"close_price": "58234.10",
					"quote":       "USDC",
				},
			},
			want: "🔔 [mexc] Signal de retournement haussier — BTC/USDC (1h)\n" +
				"Étoile du matin" +
				"\nClôture : 58234.10 USDC" +
				"\nCreux possible. /status ou bouton 🛒 Acheter pour agir.",
		},
		{
			name: "alerte erreur",
			event: notify.Event{
				Kind:  notify.KindError,
				Title: "Le bot rencontre des erreurs (3)",
				Text:  "failed to place order: insufficient balance",
			},
			want: "⚠️ [mexc] Le bot rencontre des erreurs (3) — ouvre /status" +
				"\nfailed to place order: insufficient balance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := n.render(tt.event); got != tt.want {
				t.Errorf("render() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// Un type d'événement inconnu doit rester diffusable : on retombe sur le résumé
// neutre plutôt que d'envoyer un message vide.
func TestNotifierRenderUnknownKind(t *testing.T) {
	n := &notifier{exchange: "mexc"}

	got := n.render(notify.Event{
		Kind:  notify.Kind("bot_silent"),
		Title: "Bot silencieux depuis 15 min",
		Text:  "aucun snapshot reçu",
	})

	want := "[mexc] Bot silencieux depuis 15 min\naucun snapshot reçu"
	if got != want {
		t.Errorf("render() = %q, want %q", got, want)
	}
}
