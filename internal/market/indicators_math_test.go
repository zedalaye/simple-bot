package market

import (
	"math"
	"testing"
)

// goldenCloses est une série déterministe servant de référence pour verrouiller
// la math des indicateurs (live + backtest partagent ces fonctions). Si une de
// ces valeurs change, c'est qu'un calcul a dérivé — à investiguer.
func goldenCloses() []float64 {
	closes := make([]float64, 60)
	for i := range closes {
		closes[i] = 100 + 10*math.Sin(float64(i)/5.0) + float64(i)*0.3
	}
	return closes
}

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s = %.10f, attendu %.10f (écart %.2e)", name, got, want, math.Abs(got-want))
	}
}

func TestRSIValueGolden(t *testing.T) {
	got, err := RSIValue(goldenCloses(), 14)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, "RSI", got, 51.3591722654)
}

func TestVolatilityValueGolden(t *testing.T) {
	got, err := VolatilityValue(goldenCloses(), 7)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, "Volatility", got, 0.7006436912)
}

func TestEMAValueGolden(t *testing.T) {
	got, err := EMAValue(goldenCloses(), 14)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, "EMA", got, 109.9294503907)
}

func TestMACDValueGolden(t *testing.T) {
	macd, signal, hist, err := MACDValue(goldenCloses(), 12, 26, 9)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, "MACD", macd, -1.1491258002)
	approx(t, "Signal", signal, -0.6309636013)
	approx(t, "Histogram", hist, -0.5181621990)
}

func TestIndicatorErrorsOnInsufficientData(t *testing.T) {
	if _, err := RSIValue([]float64{1, 2, 3}, 14); err == nil {
		t.Error("RSIValue aurait dû échouer sur données insuffisantes")
	}
	if _, err := EMAValue([]float64{1, 2, 3}, 14); err == nil {
		t.Error("EMAValue aurait dû échouer sur données insuffisantes")
	}
}
