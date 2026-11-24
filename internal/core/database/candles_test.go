package database

import "testing"

// Une bougie enregistrée en cours de formation doit être complétée par les collectes
// suivantes (et non figée dans son premier état, ce que faisait INSERT OR IGNORE).
func TestSaveCandle_UpsertCompletesInProgressCandle(t *testing.T) {
	db := newTestDB(t)
	const pair, tf, ts = "BTC/USDC", "1h", int64(1764010800000)

	// Première collecte, 1 min après l'ouverture : bougie quasi vide.
	if err := db.SaveCandle(pair, tf, ts, 59812.2, 59826.1, 59783.4, 59805.8, 0.098); err != nil {
		t.Fatalf("SaveCandle (partielle) : %v", err)
	}
	// Collecte après clôture : valeurs définitives.
	if err := db.SaveCandle(pair, tf, ts, 59812.2, 60007.1, 59607.5, 59609.3, 12.915); err != nil {
		t.Fatalf("SaveCandle (finale) : %v", err)
	}

	candles, err := db.GetCandles(pair, tf, 10)
	if err != nil {
		t.Fatalf("GetCandles : %v", err)
	}
	if len(candles) != 1 {
		t.Fatalf("attendu 1 bougie (pas de doublon), obtenu %d", len(candles))
	}
	c := candles[0]
	if c.HighPrice != 60007.1 || c.LowPrice != 59607.5 || c.ClosePrice != 59609.3 || c.Volume != 12.915 {
		t.Errorf("bougie non mise à jour : %+v", c)
	}
}
