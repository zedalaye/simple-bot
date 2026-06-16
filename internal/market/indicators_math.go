package market

import (
	"fmt"

	"github.com/cinar/indicator/v2/momentum"
	"github.com/cinar/indicator/v2/trend"
	"github.com/cinar/indicator/v2/volatility"
)

// Ce fichier centralise le CALCUL PUR des indicateurs (sur des slices de prix de
// clôture), indépendamment de la provenance des bougies. Il est l'unique source
// de vérité partagée par :
//   - *market.Calculator : exécution réelle, bougies fraîches lues en base ;
//   - le package backtest : bougies historiques rejouées « as-of ».
//
// IMPORTANT — fenêtrage : le lissage de Wilder (RSI/RMA) est récursif, donc le
// nombre de bougies fourni influence le résultat. Les helpers *Window ci-dessous
// définissent combien de bougies charger ; live et backtest DOIVENT utiliser la
// même fenêtre pour produire des valeurs identiques.

// RSIWindow retourne le nombre de bougies à charger pour un RSI de période donnée.
func RSIWindow(period int) int { return period * 3 }

// VolatilityWindow retourne le nombre de bougies à charger pour la volatilité.
func VolatilityWindow(period int) int { return period * 2 }

// EMAWindow retourne le nombre de bougies à charger pour une EMA.
func EMAWindow(period int) int { return period * 3 }

// MACDWindow retourne le nombre de bougies à charger pour un MACD (basé sur la
// période lente).
func MACDWindow(slowPeriod int) int { return slowPeriod * 3 }

// RSIValue calcule le RSI (Wilder) sur la série de clôtures fournie et renvoie la
// dernière valeur. Identique à l'implémentation cinar utilisée en production.
func RSIValue(closes []float64, period int) (float64, error) {
	if len(closes) < period+1 {
		return 0, fmt.Errorf("insufficient candles for RSI calculation: need %d, got %d", period+1, len(closes))
	}

	rsi := momentum.NewRsiWithPeriod[float64](period)
	inputChan := make(chan float64, len(closes))
	for _, price := range closes {
		inputChan <- price
	}
	close(inputChan)

	var rsiValues []float64
	for value := range rsi.Compute(inputChan) {
		rsiValues = append(rsiValues, value)
	}
	if len(rsiValues) == 0 {
		return 0, fmt.Errorf("RSI calculation returned no values")
	}
	return rsiValues[len(rsiValues)-1], nil
}

// VolatilityValue calcule la volatilité (écart-type des rendements, en %) sur la
// série de clôtures fournie : on dérive les rendements, on garde les `period`
// derniers, puis MovingStd. Identique à l'implémentation de production.
func VolatilityValue(closes []float64, period int) (float64, error) {
	if len(closes) < 2 {
		return 0, fmt.Errorf("need at least 2 prices for volatility calculation")
	}

	returns := make([]float64, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		returns[i-1] = (closes[i] - closes[i-1]) / closes[i-1]
	}

	if len(returns) < period {
		period = len(returns)
	}
	recentReturns := returns[len(returns)-period:]

	movingStd := volatility.NewMovingStdWithPeriod[float64](len(recentReturns))
	inputChan := make(chan float64, len(recentReturns))
	for _, returnVal := range recentReturns {
		inputChan <- returnVal
	}
	close(inputChan)

	var stdValues []float64
	for value := range movingStd.Compute(inputChan) {
		stdValues = append(stdValues, value)
	}
	if len(stdValues) == 0 {
		return 0, fmt.Errorf("volatility calculation returned no values")
	}
	return stdValues[len(stdValues)-1] * 100, nil
}

// EMAValue calcule l'EMA sur la série de clôtures fournie et renvoie la dernière
// valeur. Identique à l'implémentation de production.
func EMAValue(closes []float64, period int) (float64, error) {
	if len(closes) < period {
		return 0, fmt.Errorf("insufficient candles for EMA calculation: need %d, got %d", period, len(closes))
	}

	ema := trend.NewEmaWithPeriod[float64](period)
	inputChan := make(chan float64, len(closes))
	for _, price := range closes {
		inputChan <- price
	}
	close(inputChan)

	var emaValues []float64
	for value := range ema.Compute(inputChan) {
		emaValues = append(emaValues, value)
	}
	if len(emaValues) == 0 {
		return 0, fmt.Errorf("EMA calculation returned no values")
	}
	return emaValues[len(emaValues)-1], nil
}

// MACDValue calcule le MACD sur la série de clôtures fournie et renvoie les
// dernières valeurs (macd, signal, histogramme). Identique à la production.
func MACDValue(closes []float64, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, histogram float64, err error) {
	if len(closes) < slowPeriod+signalPeriod {
		return 0, 0, 0, fmt.Errorf("insufficient candles for MACD calculation: need %d, got %d",
			slowPeriod+signalPeriod, len(closes))
	}

	macdIndicator := trend.NewMacd[float64]()
	inputChan := make(chan float64, len(closes))
	for _, price := range closes {
		inputChan <- price
	}
	close(inputChan)

	macdChan, signalChan := macdIndicator.Compute(inputChan)

	// Les deux canaux sont alimentés en parallèle par cinar : il faut les drainer
	// concurremment, sinon le producteur se bloque en écrivant sur le canal non lu
	// (interblocage). On lit `signalChan` dans une goroutine pendant qu'on draine
	// `macdChan` ici.
	var signalResults []float64
	done := make(chan struct{})
	go func() {
		for value := range signalChan {
			signalResults = append(signalResults, value)
		}
		close(done)
	}()
	var macdResults []float64
	for value := range macdChan {
		macdResults = append(macdResults, value)
	}
	<-done
	if len(macdResults) == 0 || len(signalResults) == 0 {
		return 0, 0, 0, fmt.Errorf("MACD calculation returned no values")
	}

	latestMACD := macdResults[len(macdResults)-1]
	latestSignal := signalResults[len(signalResults)-1]
	return latestMACD, latestSignal, latestMACD - latestSignal, nil
}
