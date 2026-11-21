package bot

import (
	"math"
	"testing"

	"bot/internal/core/database"
)

func f(v float64) *float64 { return &v }
func s(v string) *string   { return &v }

// TestComputeFillBuyFeeInBase : achat OKX, frais prélevés en BTC. La quantité
// retenue est nette des frais et ceux-ci sont convertis en USDC au prix du trade.
func TestComputeFillBuyFeeInBase(t *testing.T) {
	trades := []Trade{
		{Amount: f(0.0006), Price: f(100000), Fee: f(0.00000048), FeeToken: s("BTC")},
		{Amount: f(0.0004), Price: f(100000), Fee: f(0.00000032), FeeToken: s("BTC")},
	}
	fill, unknown := computeFill(trades, database.Buy, "BTC", "USDC")
	if len(unknown) != 0 {
		t.Fatalf("aucune devise inattendue attendue, got %v", unknown)
	}
	if want := 0.0009992; math.Abs(fill.Amount-want) > 1e-12 {
		t.Errorf("quantité %.10f, attendue %.10f (nette des frais)", fill.Amount, want)
	}
	if want := 0.08; math.Abs(fill.Fees-want) > 1e-9 {
		t.Errorf("frais %.6f USDC, attendus %.6f", fill.Fees, want)
	}
}

// TestComputeFillSellFeeInQuote : vente, frais en USDC. La quantité reste la
// quantité vendue (le BTC est parti), les frais sont pris tels quels.
func TestComputeFillSellFeeInQuote(t *testing.T) {
	trades := []Trade{
		{Amount: f(0.0009992), Price: f(101160), Fee: f(0.0808), FeeToken: s("USDC")},
	}
	fill, _ := computeFill(trades, database.Sell, "BTC", "USDC")
	if math.Abs(fill.Amount-0.0009992) > 1e-12 {
		t.Errorf("quantité %.10f, attendue 0.0009992", fill.Amount)
	}
	if math.Abs(fill.Fees-0.0808) > 1e-12 {
		t.Errorf("frais %.6f, attendus 0.0808", fill.Fees)
	}
}

// TestComputeFillUnknownFeeCurrency : un frais dans une autre devise n'est ni
// retranché de la quantité ni comptabilisé, mais il est signalé.
func TestComputeFillUnknownFeeCurrency(t *testing.T) {
	trades := []Trade{
		{Amount: f(0.001), Price: f(100000), Fee: f(0.01), FeeToken: s("OKB")},
	}
	fill, unknown := computeFill(trades, database.Buy, "BTC", "USDC")
	if len(unknown) != 1 {
		t.Fatalf("1 devise inattendue attendue, got %v", unknown)
	}
	if fill.Amount != 0.001 || fill.Fees != 0 {
		t.Errorf("fill %+v, attendu quantité brute et frais nuls", fill)
	}
}
