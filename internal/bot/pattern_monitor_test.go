package bot

import (
	"testing"
	"time"

	"bot/internal/core/database"
	"bot/internal/market"
)

// declineThenHammer construit une série 1h en baisse régulière (close 100→91) suivie
// d'une bougie marteau. ts = base + i*1h. Renvoie aussi le timestamp de clôture du marteau.
func declineThenHammer() []database.Candle {
	const tf = int64(patternTimeframeMs)
	base := int64(1_700_000_000_000)
	var cs []database.Candle
	// 10 bougies rouges décroissantes : open = 101-i, close = 100-i.
	for i := 0; i < 10; i++ {
		open := 101 - float64(i)
		closeP := 100 - float64(i)
		cs = append(cs, database.Candle{
			Timestamp:  base + int64(i)*tf,
			OpenPrice:  open,
			HighPrice:  open + 0.2,
			LowPrice:   closeP - 0.2,
			ClosePrice: closeP,
			Volume:     100,
		})
	}
	// Marteau : petit corps en haut, longue mèche basse.
	last := base + int64(10)*tf
	cs = append(cs, database.Candle{
		Timestamp:  last,
		OpenPrice:  91.0,
		HighPrice:  91.2,
		LowPrice:   89.0,
		ClosePrice: 91.1,
		Volume:     100,
	})
	return cs
}

func TestEvaluateReversalSignal_HammerAfterDecline(t *testing.T) {
	cs := declineThenHammer()
	hammerClose := cs[len(cs)-1].Timestamp + patternTimeframeMs
	now := time.UnixMilli(hammerClose + 1000) // juste après la clôture du marteau

	p, idx, ok := evaluateReversalSignal(cs, now)
	if !ok {
		t.Fatalf("attendu un signal, got ok=false (p=%q, idx=%d)", p, idx)
	}
	if p != market.PatternHammer {
		t.Errorf("pattern = %q, attendu hammer", p)
	}
	if idx != len(cs)-1 {
		t.Errorf("closedIdx = %d, attendu %d (le marteau)", idx, len(cs)-1)
	}
}

func TestEvaluateReversalSignal_LastCandleStillForming(t *testing.T) {
	cs := declineThenHammer()
	// now PENDANT la bougie marteau : elle n'est pas clôturée → on évalue la précédente
	// (une rouge), donc pas de signal.
	now := time.UnixMilli(cs[len(cs)-1].Timestamp + 1000)

	if p, _, ok := evaluateReversalSignal(cs, now); ok {
		t.Errorf("marteau non clôturé : attendu pas de signal, got ok=true (p=%q)", p)
	}
}

func TestEvaluateReversalSignal_StaleCandleIgnored(t *testing.T) {
	cs := declineThenHammer()
	hammerClose := cs[len(cs)-1].Timestamp + patternTimeframeMs
	// now bien après (plus de 2 bougies plus tard) → garde-fou de fraîcheur : pas de notif.
	now := time.UnixMilli(hammerClose + 3*patternTimeframeMs)

	if _, _, ok := evaluateReversalSignal(cs, now); ok {
		t.Error("bougie périmée : attendu pas de signal, got ok=true")
	}
}

func TestConvictionTag(t *testing.T) {
	cases := []struct {
		rsi, vol bool
		want     string
	}{
		{true, true, "🔥 haute conviction"},
		{true, false, "⭐ conviction moyenne"},
		{false, true, "⭐ conviction moyenne"},
		{false, false, ""},
	}
	for _, tc := range cases {
		if got := convictionTag(tc.rsi, tc.vol); got != tc.want {
			t.Errorf("convictionTag(%v,%v) = %q, attendu %q", tc.rsi, tc.vol, got, tc.want)
		}
	}
}

func TestEvaluateReversalSignal_NoPattern(t *testing.T) {
	// Série plate (bougies sans forme particulière) : aucun signal.
	const tf = int64(patternTimeframeMs)
	base := int64(1_700_000_000_000)
	var cs []database.Candle
	for i := 0; i < 14; i++ {
		cs = append(cs, database.Candle{
			Timestamp:  base + int64(i)*tf,
			OpenPrice:  100,
			HighPrice:  100.5,
			LowPrice:   99.5,
			ClosePrice: 100.1,
			Volume:     100,
		})
	}
	now := time.UnixMilli(cs[len(cs)-1].Timestamp + patternTimeframeMs + 1000)
	if p, _, ok := evaluateReversalSignal(cs, now); ok {
		t.Errorf("série plate : attendu pas de signal, got ok=true (p=%q)", p)
	}
}
