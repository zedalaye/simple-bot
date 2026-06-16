package backtest

import (
	"math"
	"testing"

	"bot/internal/algorithms"
	"bot/internal/core/database"
)

const tf15 = int64(15 * 60 * 1000) // durée d'une bougie 15m en ms

// makeCandles construit des bougies 15m à partir d'une fonction de prix.
func makeCandles(n int, price func(i int) float64) []database.Candle {
	cs := make([]database.Candle, n)
	for i := 0; i < n; i++ {
		p := price(i)
		cs[i] = database.Candle{
			Pair: "BTC/USDC", Timeframe: "15m",
			Timestamp:  int64(i) * tf15,
			OpenPrice:  p,
			HighPrice:  p + 1,
			LowPrice:   p - 1,
			ClosePrice: p,
			Volume:     1,
		}
	}
	return cs
}

// TestCalculatorAsOfNoLookahead vérifie que le plus-haut « as-of » n'inclut
// jamais une bougie dont la clôture est dans le futur du curseur.
func TestCalculatorAsOfNoLookahead(t *testing.T) {
	// highs croissants : 1,2,3,... -> le plus-haut révèle l'indice vu.
	candles := makeCandles(10, func(i int) float64 { return float64(i) })
	calc := NewCalculator("BTC/USDC", map[string][]database.Candle{"15m": candles})

	// Curseur juste après la clôture de la bougie d'indice 4 (close à 5*tf15).
	calc.SetNow(candles[4].Timestamp + tf15)
	high, err := calc.CalculateRecentHigh("BTC/USDC", "15m", 100)
	if err != nil {
		t.Fatal(err)
	}
	// La bougie 4 a high = price(4)+1 = 5 ; les bougies 5+ (futures) ne doivent
	// pas être vues.
	if high != 5 {
		t.Errorf("plus-haut as-of = %.1f, attendu 5 (pas de look-ahead)", high)
	}

	// Curseur AVANT toute clôture -> erreur attendue.
	calc.SetNow(-1)
	if _, err := calc.CalculateRecentHigh("BTC/USDC", "15m", 100); err == nil {
		t.Error("attendu une erreur quand aucune bougie n'est clôturée")
	}
}

// TestEngineProducesCycles : sur un prix oscillant, le moteur doit remplir des
// achats et boucler des cycles, sans look-ahead ni explosion.
func TestEngineProducesCycles(t *testing.T) {
	// Oscillation ±2 autour de 100 -> les bas remplissent les achats limites,
	// les hauts atteignent les cibles.
	candles := makeCandles(200, func(i int) float64 { return 100 + 2*math.Sin(float64(i)/3.0) })

	threshold := 100.0 // RSI toujours sous le seuil -> achète à chaque créneau
	period := 14
	volPeriod := 7
	adj := 0.0
	strategy := database.Strategy{
		Name: "test", AlgorithmName: "rsi_dca", Enabled: true,
		RSIThreshold: &threshold, RSIPeriod: &period, RSITimeframe: "15m",
		ProfitTarget: 1.0, TrailingStopDelta: 0.1, SellOffset: 0.1,
		VolatilityPeriod: &volPeriod, VolatilityAdjustment: &adj, VolatilityTimeframe: "15m",
		QuoteAmount: 20, BuyIntervalSeconds: 3600, // 1 tentative / heure
	}

	cfg := Config{
		Pair: "BTC/USDC", Strategy: strategy, PriceTimeframe: "15m",
		FeeRate:   0.001,
		Precision: algorithms.MarketPrecision{Price: 0.01, Amount: 0.000001},
	}

	res, err := Run(cfg, map[string][]database.Candle{"15m": candles})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuysPlaced == 0 {
		t.Error("aucun achat placé")
	}
	if res.BuysFilled == 0 {
		t.Error("aucun achat rempli")
	}
	if res.CyclesClosed == 0 {
		t.Error("aucun cycle bouclé")
	}
	if res.CyclesClosed > res.BuysFilled {
		t.Errorf("incohérence : %d cycles bouclés > %d achats remplis", res.CyclesClosed, res.BuysFilled)
	}
	// Chaque cycle bouclé vend au-dessus de l'achat -> 100 % de réussite par construction.
	if res.WinRate != 100 {
		t.Errorf("win rate = %.0f%%, attendu 100%% (vente uniquement à la cible)", res.WinRate)
	}
}
