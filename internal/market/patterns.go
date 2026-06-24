package market

import (
	"math"

	"bot/internal/core/database"
)

// Ce fichier détecte des PATTERNS DE BOUGIES DE RETOURNEMENT HAUSSIER (bullish
// reversal) à partir de l'OHLC pur, indépendamment de la provenance des bougies.
// Comme indicators_math.go, c'est de la math pure et testable : aucune dépendance
// à la base ni à un exchange, pour être partagée live / backtest / scan.
//
// Idée générale : en bas d'une baisse, certaines formes de bougies trahissent que
// les vendeurs s'essoufflent et que les acheteurs reprennent la main. On ne cherche
// QUE des signaux haussiers (on achète des creux), pas les retournements baissiers.

// OHLCV est une bougie réduite à ce dont les patterns ont besoin. On évite de
// dépendre de database.Candle dans la math pour garder les tests triviaux.
type OHLCV struct {
	Open, High, Low, Close, Volume float64
}

// OHLCVFromCandles convertit les bougies de la base en série OHLCV (même ordre).
func OHLCVFromCandles(candles []database.Candle) []OHLCV {
	out := make([]OHLCV, len(candles))
	for i, c := range candles {
		out[i] = OHLCV{Open: c.OpenPrice, High: c.HighPrice, Low: c.LowPrice, Close: c.ClosePrice, Volume: c.Volume}
	}
	return out
}

// --- Helpers géométriques sur une bougie ---

// body est la taille du corps (|close - open|).
func (c OHLCV) body() float64 { return math.Abs(c.Close - c.Open) }

// rng est l'amplitude totale (high - low).
func (c OHLCV) rng() float64 { return c.High - c.Low }

// isBull : bougie verte (clôture > ouverture).
func (c OHLCV) isBull() bool { return c.Close > c.Open }

// isBear : bougie rouge (clôture < ouverture).
func (c OHLCV) isBear() bool { return c.Close < c.Open }

// upperWick : mèche haute (au-dessus du corps).
func (c OHLCV) upperWick() float64 { return c.High - math.Max(c.Open, c.Close) }

// lowerWick : mèche basse (sous le corps).
func (c OHLCV) lowerWick() float64 { return math.Min(c.Open, c.Close) - c.Low }

// bodyMid : milieu du corps réel.
func (c OHLCV) bodyMid() float64 { return (c.Open + c.Close) / 2 }

// Pattern identifie un type de signal détecté.
type Pattern string

const (
	PatternNone             Pattern = ""
	PatternHammer           Pattern = "hammer"
	PatternBullishEngulfing Pattern = "bullish_engulfing"
	PatternPiercingLine     Pattern = "piercing_line"
	PatternMorningStar      Pattern = "morning_star"
)

// AllBullishPatterns liste les patterns haussiers reconnus (ordre = priorité de
// détection dans DetectBullishReversal).
var AllBullishPatterns = []Pattern{
	PatternMorningStar,
	PatternBullishEngulfing,
	PatternPiercingLine,
	PatternHammer,
}

// IsHammer reconnaît un MARTEAU sur la dernière bougie de c : petit corps en haut,
// longue mèche basse (≥ 2× le corps), mèche haute négligeable. C'est le rejet d'une
// plongée intraday : le prix a chuté puis a été racheté avant la clôture.
//
// Conditions (sur la dernière bougie) :
//   - amplitude non nulle ;
//   - corps petit relativement à l'amplitude (≤ 35 %) ;
//   - mèche basse ≥ 2× le corps ;
//   - mèche haute ≤ 15 % de l'amplitude.
//
// La couleur n'est pas imposée (un marteau vert est un peu plus fort, mais les deux
// comptent). N'impose PAS de contexte de tendance : cf. PrecededByDecline.
func IsHammer(c []OHLCV) bool {
	if len(c) < 1 {
		return false
	}
	last := c[len(c)-1]
	r := last.rng()
	if r <= 0 {
		return false
	}
	b := last.body()
	if b <= 0 {
		return false // doji parfait : pas un marteau (corps inexistant)
	}
	return b <= 0.35*r &&
		last.lowerWick() >= 2*b &&
		last.upperWick() <= 0.15*r
}

// IsBullishEngulfing reconnaît un AVALEMENT HAUSSIER sur les deux dernières bougies :
// une bougie rouge suivie d'une bougie verte dont le corps englobe celui de la rouge
// (ouvre sous sa clôture, clôture au-dessus de son ouverture). Les acheteurs ont
// effacé d'un coup la séance vendeuse précédente.
func IsBullishEngulfing(c []OHLCV) bool {
	if len(c) < 2 {
		return false
	}
	prev := c[len(c)-2]
	curr := c[len(c)-1]
	if !prev.isBear() || !curr.isBull() {
		return false
	}
	if prev.body() <= 0 {
		return false // avaler un quasi-doji n'est pas significatif
	}
	// Le corps vert recouvre le corps rouge.
	return curr.Open <= prev.Close && curr.Close >= prev.Open && curr.body() > prev.body()
}

// IsPiercingLine reconnaît une LIGNE PERÇANTE sur les deux dernières bougies : une
// rouge suivie d'une verte qui ouvre plus bas (sous la clôture précédente) puis
// remonte clôturer au-dessus du MILIEU du corps rouge, sans toutefois le dépasser
// entièrement (sinon c'est un avalement). Retournement partiel mais net.
func IsPiercingLine(c []OHLCV) bool {
	if len(c) < 2 {
		return false
	}
	prev := c[len(c)-2]
	curr := c[len(c)-1]
	if !prev.isBear() || !curr.isBull() || prev.body() <= 0 {
		return false
	}
	return curr.Open < prev.Close && // ouvre sous la clôture rouge (faiblesse persistante à l'ouverture)
		curr.Close > prev.bodyMid() && // clôture au-dessus du milieu du corps rouge
		curr.Close < prev.Open // mais pas au-dessus de l'ouverture rouge (sinon avalement)
}

// IsMorningStar reconnaît une ÉTOILE DU MATIN sur les trois dernières bougies :
// une grande rouge, puis une petite bougie d'indécision (l'étoile), puis une grande
// verte qui reprend au-dessus du milieu de la première. Schéma de retournement en
// trois temps : capitulation, pause, reprise.
func IsMorningStar(c []OHLCV) bool {
	if len(c) < 3 {
		return false
	}
	first := c[len(c)-3]
	star := c[len(c)-2]
	last := c[len(c)-1]
	if !first.isBear() || first.body() <= 0 {
		return false
	}
	// L'étoile a un petit corps relativement à la première bougie.
	if star.body() > 0.5*first.body() {
		return false
	}
	// La troisième est verte et reprend au-dessus du milieu du corps de la première.
	return last.isBull() && last.Close > first.bodyMid()
}

// DetectBullishReversal renvoie le premier pattern haussier détecté sur la DERNIÈRE
// bougie de c (par priorité décroissante de spécificité), ou PatternNone. C'est de
// la détection de FORME pure : appliquer le contexte (PrecededByDecline) et le volume
// au niveau appelant selon le besoin.
func DetectBullishReversal(c []OHLCV) Pattern {
	switch {
	case IsMorningStar(c):
		return PatternMorningStar
	case IsBullishEngulfing(c):
		return PatternBullishEngulfing
	case IsPiercingLine(c):
		return PatternPiercingLine
	case IsHammer(c):
		return PatternHammer
	default:
		return PatternNone
	}
}

// PrecededByDecline indique si le prix a BAISSÉ sur les `lookback` bougies précédant
// immédiatement la dernière bougie de c (la clôture `lookback` bougies avant la
// dernière est supérieure à la clôture juste avant la dernière). Sert de filtre de
// contexte : un retournement haussier n'a de sens qu'après une baisse.
func PrecededByDecline(c []OHLCV, lookback int) bool {
	n := len(c)
	if lookback <= 0 || n < lookback+2 {
		return false
	}
	// clôture avant la bougie de signal, vs clôture lookback bougies plus tôt.
	before := c[n-2].Close
	earlier := c[n-2-lookback].Close
	return earlier > before
}

// VolumeSpike indique si le volume de la dernière bougie dépasse `mult` fois la
// moyenne des `lookback` bougies précédentes. Un retournement appuyé par un volume
// au-dessus de la normale est réputé plus fiable. Renvoie false si le volume n'est
// pas renseigné (somme nulle) pour ne pas filtrer à tort.
func VolumeSpike(c []OHLCV, lookback int, mult float64) bool {
	n := len(c)
	if lookback <= 0 || n < lookback+1 {
		return false
	}
	var sum float64
	for i := n - 1 - lookback; i < n-1; i++ {
		sum += c[i].Volume
	}
	if sum <= 0 {
		return false
	}
	avg := sum / float64(lookback)
	return c[n-1].Volume >= mult*avg
}
