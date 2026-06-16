package algorithms

import (
	"os"
	"testing"

	"bot/internal/core/database"
	"bot/internal/logger"
)

// TestMain initialise le logger (les algos journalisent globalement) au niveau
// error pour éviter tout nil deref pendant les tests.
func TestMain(m *testing.M) {
	_ = logger.InitLogger("error", "")
	os.Exit(m.Run())
}

// stubCalc est un IndicatorCalculator contrôlé : il renvoie une volatilité
// « courante » ou « de référence » selon la période demandée, ce qui permet de
// tester la cible de profit dynamique sans données réelles.
type stubCalc struct {
	rsi       float64
	volShort  float64 // volatilité courante (période = VolatilityPeriod)
	volRef    float64 // volatilité de référence (période = VolatilityPeriod * mult)
	refPeriod int
}

func (s stubCalc) CalculateRSI(pair, tf string, period int) (float64, error) { return s.rsi, nil }
func (s stubCalc) CalculateEMA(pair, tf string, period int) (float64, error) { return 0, nil }
func (s stubCalc) CalculateVolatility(pair, tf string, period int) (float64, error) {
	if period == s.refPeriod {
		return s.volRef, nil
	}
	return s.volShort, nil
}
func (s stubCalc) CalculateRecentHigh(pair, tf string, periods int) (float64, error) { return 0, nil }
func (s stubCalc) CalculateMACD(pair, tf string, fast, slow, signal int) (float64, float64, float64, error) {
	return 0, 0, 0, nil
}

func fp(v float64) *float64 { return &v }
func ip(v int) *int         { return &v }

// dynamicProfitFromSignal retrouve la cible de profit effective à partir des prix
// pré-calculés du signal d'achat.
func dynamicProfitFromSignal(sig BuySignal) float64 {
	return sig.TargetPrice/sig.LimitPrice - 1
}

func baseStrategy(adj float64) database.Strategy {
	return database.Strategy{
		Name: "t", AlgorithmName: "rsi_dca", Enabled: true,
		RSIThreshold: fp(60), RSIPeriod: ip(14), RSITimeframe: "15m",
		ProfitTarget:         2.0,
		TrailingStopDelta:    0.1,
		SellOffset:           0.1,
		VolatilityPeriod:     ip(7),
		VolatilityAdjustment: fp(adj),
		VolatilityTimeframe:  "4h",
		QuoteAmount:          20,
		BuyIntervalSeconds:   86400,
	}
}

// TestDynamicProfitPivotsOnReference : la cible monte au-dessus de la base quand
// la vol courante dépasse la référence, et descend en-dessous sinon. C'est la
// correction du pivot (référence de volatilité, pas l'objectif de profit).
func TestDynamicProfitPivotsOnReference(t *testing.T) {
	algo := &RSI_DCA{}
	ctx := TradingContext{
		ExchangeName: "test", Pair: "BTC/USDC", CurrentPrice: 100,
		Precision: MarketPrecision{Price: 0.0001, Amount: 0.000001},
	}
	refPeriod := 7 * volatilityReferenceMultiplier
	base := 0.02 // ProfitTarget 2% en fraction

	// Vol courante > référence -> cible AU-DESSUS de la base.
	ctx.Calculator = stubCalc{rsi: 40, volShort: 1.5, volRef: 0.8, refPeriod: refPeriod}
	sig, err := algo.ShouldBuy(ctx, baseStrategy(50))
	if err != nil {
		t.Fatal(err)
	}
	if got := dynamicProfitFromSignal(sig); got <= base {
		t.Errorf("forte volatilité : cible %.4f attendue > base %.4f", got, base)
	}

	// Vol courante < référence -> cible EN-DESSOUS de la base.
	ctx.Calculator = stubCalc{rsi: 40, volShort: 0.4, volRef: 0.8, refPeriod: refPeriod}
	sig, err = algo.ShouldBuy(ctx, baseStrategy(50))
	if err != nil {
		t.Fatal(err)
	}
	if got := dynamicProfitFromSignal(sig); got >= base {
		t.Errorf("faible volatilité : cible %.4f attendue < base %.4f", got, base)
	}
}

// TestDynamicProfitDisabledKeepsBase : adjustment = 0 -> cible == base (rétro-compat).
func TestDynamicProfitDisabledKeepsBase(t *testing.T) {
	algo := &RSI_DCA{}
	ctx := TradingContext{
		ExchangeName: "test", Pair: "BTC/USDC", CurrentPrice: 100,
		Precision:  MarketPrecision{Price: 0.0001, Amount: 0.000001},
		Calculator: stubCalc{rsi: 40, volShort: 1.5, volRef: 0.8, refPeriod: 7 * volatilityReferenceMultiplier},
	}
	sig, err := algo.ShouldBuy(ctx, baseStrategy(0))
	if err != nil {
		t.Fatal(err)
	}
	got := dynamicProfitFromSignal(sig)
	if d := got - 0.02; d < -0.0005 || d > 0.0005 {
		t.Errorf("ajustement nul : cible %.5f attendue ≈ base 0.02", got)
	}
}
