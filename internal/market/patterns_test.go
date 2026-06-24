package market

import "testing"

// mk construit une bougie OHLC (volume nul) pour les tests de forme.
func mk(o, h, l, c float64) OHLCV { return OHLCV{Open: o, High: h, Low: l, Close: c} }

// mkv construit une bougie OHLC avec volume.
func mkv(o, h, l, c, v float64) OHLCV { return OHLCV{Open: o, High: h, Low: l, Close: c, Volume: v} }

func TestIsHammer(t *testing.T) {
	cases := []struct {
		name string
		c    OHLCV
		want bool
	}{
		// Petit corps en haut, longue mèche basse, mèche haute négligeable.
		{"marteau vert", mk(10.0, 10.3, 9.0, 10.2), true},
		{"marteau rouge", mk(10.2, 10.3, 9.0, 10.0), true},
		// Longue mèche HAUTE = étoile filante, pas un marteau.
		{"shooting star", mk(10.0, 11.0, 9.9, 10.2), false},
		// Gros corps : la mèche ne domine pas.
		{"grand corps", mk(10.0, 11.1, 9.9, 11.0), false},
		// Mèche basse trop courte (< 2× corps).
		{"meche basse courte", mk(10.0, 10.3, 9.85, 10.2), false},
	}
	for _, tc := range cases {
		if got := IsHammer([]OHLCV{tc.c}); got != tc.want {
			t.Errorf("%s : IsHammer = %v, attendu %v", tc.name, got, tc.want)
		}
	}
}

func TestIsBullishEngulfing(t *testing.T) {
	cases := []struct {
		name       string
		prev, curr OHLCV
		want       bool
	}{
		{"avalement net", mk(11, 11.1, 9.9, 10), mk(9.9, 11.2, 9.8, 11.1), true},
		{"verte n'englobe pas", mk(11, 11.1, 9.9, 10), mk(10.1, 10.6, 10.0, 10.5), false},
		{"deux vertes", mk(10, 10.6, 9.9, 10.5), mk(9.9, 11.2, 9.8, 11.1), false},
		{"corps verte plus petit", mk(11, 11.1, 9.9, 10), mk(10.4, 10.9, 10.3, 10.8), false},
	}
	for _, tc := range cases {
		if got := IsBullishEngulfing([]OHLCV{tc.prev, tc.curr}); got != tc.want {
			t.Errorf("%s : IsBullishEngulfing = %v, attendu %v", tc.name, got, tc.want)
		}
	}
}

func TestIsPiercingLine(t *testing.T) {
	cases := []struct {
		name       string
		prev, curr OHLCV
		want       bool
	}{
		// Ouvre sous la clôture rouge, clôture au-dessus du milieu (10.5) sans dépasser l'ouverture (11).
		{"ligne percante", mk(11, 11.1, 9.9, 10), mk(9.8, 10.8, 9.7, 10.7), true},
		// Clôture sous le milieu du corps rouge : pas assez de pénétration.
		{"penetration faible", mk(11, 11.1, 9.9, 10), mk(9.8, 10.5, 9.7, 10.4), false},
		// Clôture au-dessus de l'ouverture rouge : c'est un avalement, pas une perçante.
		{"trop = avalement", mk(11, 11.3, 9.9, 10), mk(9.8, 11.3, 9.7, 11.2), false},
	}
	for _, tc := range cases {
		if got := IsPiercingLine([]OHLCV{tc.prev, tc.curr}); got != tc.want {
			t.Errorf("%s : IsPiercingLine = %v, attendu %v", tc.name, got, tc.want)
		}
	}
}

func TestIsMorningStar(t *testing.T) {
	first := mk(12, 12.1, 9.9, 10) // grande rouge, milieu = 11
	cases := []struct {
		name             string
		star, last       OHLCV
		want             bool
	}{
		{"etoile du matin", mk(9.9, 10.0, 9.7, 9.8), mk(9.9, 11.3, 9.8, 11.2), true},
		// Dernière clôture sous le milieu de la première : reprise insuffisante.
		{"reprise faible", mk(9.9, 10.0, 9.7, 9.8), mk(9.9, 10.9, 9.8, 10.5), false},
		// Corps de l'étoile trop grand (pas d'indécision).
		{"etoile trop grande", mk(9.9, 10.1, 8.4, 8.5), mk(8.5, 11.3, 8.4, 11.2), false},
	}
	for _, tc := range cases {
		if got := IsMorningStar([]OHLCV{first, tc.star, tc.last}); got != tc.want {
			t.Errorf("%s : IsMorningStar = %v, attendu %v", tc.name, got, tc.want)
		}
	}
}

func TestDetectBullishReversalPriority(t *testing.T) {
	// Un avalement haussier doit être reconnu par le détecteur global.
	series := []OHLCV{mk(11, 11.1, 9.9, 10), mk(9.9, 11.2, 9.8, 11.1)}
	if got := DetectBullishReversal(series); got != PatternBullishEngulfing {
		t.Errorf("DetectBullishReversal = %q, attendu %q", got, PatternBullishEngulfing)
	}
	// Bougie banale : aucun pattern.
	none := []OHLCV{mk(10, 10.5, 9.8, 10.1), mk(10.1, 10.6, 10.0, 10.2)}
	if got := DetectBullishReversal(none); got != PatternNone {
		t.Errorf("DetectBullishReversal = %q, attendu PatternNone", got)
	}
}

func TestPrecededByDecline(t *testing.T) {
	// Série décroissante puis bougie de signal : déclin détecté.
	down := []OHLCV{mk(20, 20, 19, 19), mk(19, 19, 18, 18), mk(18, 18, 17, 17), mk(17, 17, 16, 16.5)}
	if !PrecededByDecline(down, 2) {
		t.Error("PrecededByDecline = false sur une série en baisse, attendu true")
	}
	// Série croissante : pas de déclin.
	up := []OHLCV{mk(10, 11, 10, 11), mk(11, 12, 11, 12), mk(12, 13, 12, 13), mk(13, 14, 13, 13.5)}
	if PrecededByDecline(up, 2) {
		t.Error("PrecededByDecline = true sur une série en hausse, attendu false")
	}
}

func TestVolumeSpike(t *testing.T) {
	// Volumes plats à 100 puis pic à 300 : > 2× la moyenne.
	c := []OHLCV{mkv(1, 1, 1, 1, 100), mkv(1, 1, 1, 1, 100), mkv(1, 1, 1, 1, 100), mkv(1, 1, 1, 1, 300)}
	if !VolumeSpike(c, 3, 2.0) {
		t.Error("VolumeSpike = false sur un pic 3×, attendu true")
	}
	// Volume dans la norme : pas de pic.
	flat := []OHLCV{mkv(1, 1, 1, 1, 100), mkv(1, 1, 1, 1, 100), mkv(1, 1, 1, 1, 100), mkv(1, 1, 1, 1, 110)}
	if VolumeSpike(flat, 3, 2.0) {
		t.Error("VolumeSpike = true sur un volume normal, attendu false")
	}
	// Volume non renseigné : ne filtre pas (false).
	novol := []OHLCV{mk(1, 1, 1, 1), mk(1, 1, 1, 1), mk(1, 1, 1, 1), mk(1, 1, 1, 1)}
	if VolumeSpike(novol, 3, 2.0) {
		t.Error("VolumeSpike = true sans volume, attendu false")
	}
}
