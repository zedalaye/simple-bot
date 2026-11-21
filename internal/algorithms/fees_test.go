package algorithms

import (
	"math"
	"testing"
)

// TestTargetIncludesRoundTripFees : la cible de vente couvre l'aller-retour maker
// en plus de ProfitTarget, qui reste ainsi une marge nette.
func TestTargetIncludesRoundTripFees(t *testing.T) {
	algo := &RSI_DCA{}
	ctx := TradingContext{
		ExchangeName: "test", Pair: "BTC/USDC", CurrentPrice: 100,
		Precision:  MarketPrecision{Price: 0.0001, Amount: 0.000001},
		Calculator: stubCalc{rsi: 40, volShort: 1.5, volRef: 0.8, refPeriod: 7 * volatilityReferenceMultiplier},
		MakerFee:   0.0008,
	}
	sig, err := algo.ShouldBuy(ctx, baseStrategy(0))
	if err != nil {
		t.Fatal(err)
	}
	got := dynamicProfitFromSignal(sig)
	if want := 0.02 + 2*0.0008; math.Abs(got-want) > 0.00005 {
		t.Errorf("cible %.5f, attendue ≈ %.5f (2 %% + 2 × 0,08 %%)", got, want)
	}
}

// TestFeesAddedAfterFloor : même quand la volatilité écrase la cible jusqu'au
// plancher de 0,1 %, les frais s'y ajoutent ; le cycle ne peut pas boucler à perte.
func TestFeesAddedAfterFloor(t *testing.T) {
	algo := &RSI_DCA{}
	ctx := TradingContext{
		ExchangeName: "test", Pair: "BTC/USDC", CurrentPrice: 100,
		Precision: MarketPrecision{Price: 0.0001, Amount: 0.000001},
		// Vol courante très inférieure à la référence + ajustement massif => plancher
		Calculator: stubCalc{rsi: 40, volShort: 0.1, volRef: 5, refPeriod: 7 * volatilityReferenceMultiplier},
		MakerFee:   0.0008,
	}
	sig, err := algo.ShouldBuy(ctx, baseStrategy(1000))
	if err != nil {
		t.Fatal(err)
	}
	got := dynamicProfitFromSignal(sig)
	if want := 0.001 + 2*0.0008; math.Abs(got-want) > 0.00005 {
		t.Errorf("cible plancher %.5f, attendue ≈ %.5f (0,1 %% + frais)", got, want)
	}
}

func TestRoundAmountDown(t *testing.T) {
	p := MarketPrecision{Amount: 0.00000001}
	cases := []struct{ in, want float64 }{
		{0.0009992, 0.0009992},    // déjà au pas : inchangé malgré le flottant
		{0.000999259, 0.00099925}, // au pas inférieur, jamais au-dessus
		{0.001 - 0.001*0.0008, 0.0009992},
	}
	for _, c := range cases {
		if got := RoundAmountDown(c.in, p); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("RoundAmountDown(%.10f) = %.10f, attendu %.10f", c.in, got, c.want)
		}
	}
}
