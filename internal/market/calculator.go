package market

import (
	"bot/internal/core/database"
	"bot/internal/logger"
	"fmt"
	"math"

	"github.com/cinar/indicator/v2/momentum"
	"github.com/cinar/indicator/v2/trend"
	"github.com/cinar/indicator/v2/volatility"
)

// Calculator handles technical indicator calculations using cached candle data
type Calculator struct {
	db        *database.DB
	collector *MarketDataCollector
}

// NewCalculator creates a new technical indicators calculator
func NewCalculator(db *database.DB, collector *MarketDataCollector) *Calculator {
	return &Calculator{
		db:        db,
		collector: collector,
	}
}

// CalculateRSI computes RSI using the github.com/cinar/indicator library
func (c *Calculator) CalculateRSI(pair, timeframe string, period int) (float64, error) {
	logger.Debugf("Calculating RSI for %s/%s with period %d", pair, timeframe, period)

	// Ensure we have enough candles (RSI needs at least period+1 candles)
	requiredCandles := period * 3 // Get more data for accurate RSI calculation
	err := c.collector.EnsureCandlesAvailable(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, fmt.Errorf("failed to ensure candles availability: %w", err)
	}

	// Get candles from database
	candles, err := c.db.GetCandles(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, fmt.Errorf("failed to get candles for RSI: %w", err)
	}

	if len(candles) < period+1 {
		return 0, fmt.Errorf("insufficient candles for RSI calculation: need %d, got %d", period+1, len(candles))
	}

	// Convert candles to closes array for indicator library
	closes := make([]float64, len(candles))
	for i, candle := range candles {
		closes[i] = candle.ClosePrice
	}

	// Calculate RSI using indicator v2 library with channels (following your example)
	rsi := momentum.NewRsiWithPeriod[float64](period)

	// Create input channel and send closing prices
	inputChan := make(chan float64, len(closes))
	for _, price := range closes {
		inputChan <- price
	}
	close(inputChan)

	// Compute RSI values using the channel API
	rsiChan := rsi.Compute(inputChan)

	// Collect all RSI values
	var rsiValues []float64
	for value := range rsiChan {
		rsiValues = append(rsiValues, value)
	}

	if len(rsiValues) == 0 {
		return 0, fmt.Errorf("RSI calculation returned no values")
	}

	// Return the latest RSI value
	latestRSI := rsiValues[len(rsiValues)-1]
	logger.Debugf("Calculated RSI: %.2f", latestRSI)

	return latestRSI, nil
}

// CalculateVolatility calculates price volatility (standard deviation)
func (c *Calculator) CalculateVolatility(pair, timeframe string, period int) (float64, error) {
	logger.Debugf("Calculating volatility for %s/%s with period %d", pair, timeframe, period)

	// Ensure we have enough candles
	requiredCandles := period * 2
	err := c.collector.EnsureCandlesAvailable(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, fmt.Errorf("failed to ensure candles availability: %w", err)
	}

	// Get candles from database
	candles, err := c.db.GetCandles(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, fmt.Errorf("failed to get candles for volatility: %w", err)
	}

	if len(candles) < period {
		return 0, fmt.Errorf("insufficient candles for volatility calculation: need %d, got %d", period, len(candles))
	}

	// Convert candles to closes array
	closes := make([]float64, len(candles))
	for i, candle := range candles {
		closes[i] = candle.ClosePrice
	}

	// Calculate volatility using manual standard deviation (helper API unclear in v2)
	if len(closes) < period {
		return 0, fmt.Errorf("insufficient data for volatility calculation")
	}

	// Use the most recent 'period' candles
	recentCloses := closes[len(closes)-period:]

	// Calculate returns
	returns := make([]float64, len(recentCloses)-1)
	for i := 1; i < len(recentCloses); i++ {
		returns[i-1] = (recentCloses[i] - recentCloses[i-1]) / recentCloses[i-1]
	}

	// Calculate mean
	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	// Calculate variance
	var variance float64
	for _, r := range returns {
		variance += math.Pow(r-mean, 2)
	}
	variance /= float64(len(returns))

	// Return standard deviation as percentage
	latestVolatility := math.Sqrt(variance) * 100
	logger.Debugf("Calculated volatility: %.2f%%", latestVolatility)

	return latestVolatility, nil
}

// CalculateMACD computes MACD using indicator v2 library with channels
func (c *Calculator) CalculateMACD(pair, timeframe string, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, histogram float64, err error) {
	logger.Debugf("Calculating MACD for %s/%s (fast: %d, slow: %d, signal: %d)",
		pair, timeframe, fastPeriod, slowPeriod, signalPeriod)

	// Ensure we have enough candles
	requiredCandles := slowPeriod * 3
	err = c.collector.EnsureCandlesAvailable(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to ensure candles availability: %w", err)
	}

	// Get candles from database
	candles, err := c.db.GetCandles(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to get candles for MACD: %w", err)
	}

	if len(candles) < slowPeriod+signalPeriod {
		return 0, 0, 0, fmt.Errorf("insufficient candles for MACD calculation: need %d, got %d",
			slowPeriod+signalPeriod, len(candles))
	}

	// Convert to closes array
	closes := make([]float64, len(candles))
	for i, candle := range candles {
		closes[i] = candle.ClosePrice
	}

	// Create MACD instance following v2 API pattern
	macdIndicator := trend.NewMacd[float64]()

	// Create input channel and send closing prices
	inputChan := make(chan float64, len(closes))
	for _, price := range closes {
		inputChan <- price
	}
	close(inputChan)

	// Compute MACD values using channels (returns 2 values: macd and signal)
	macdChan, signalChan := macdIndicator.Compute(inputChan)

	// Collect MACD values from first channel
	var macdResults []float64
	for value := range macdChan {
		macdResults = append(macdResults, value)
	}

	// Collect signal values from second channel
	var signalResults []float64
	for value := range signalChan {
		signalResults = append(signalResults, value)
	}

	if len(macdResults) == 0 || len(signalResults) == 0 {
		return 0, 0, 0, fmt.Errorf("MACD calculation returned no values")
	}

	// Get latest values
	latestMACD := macdResults[len(macdResults)-1]
	latestSignal := signalResults[len(signalResults)-1]
	histogramVal := latestMACD - latestSignal

	logger.Debugf("Calculated MACD: %.4f, Signal: %.4f, Histogram: %.4f", latestMACD, latestSignal, histogramVal)
	return latestMACD, latestSignal, histogramVal, nil
}

// CalculateBollingerBands computes Bollinger Bands using indicator v2 library with channels
func (c *Calculator) CalculateBollingerBands(pair, timeframe string, period int, k float64) (upper, middle, lower float64, err error) {
	logger.Debugf("Calculating Bollinger Bands for %s/%s (period: %d, k: %.1f)", pair, timeframe, period, k)

	// Ensure we have enough candles
	requiredCandles := period * 2
	err = c.collector.EnsureCandlesAvailable(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to ensure candles availability: %w", err)
	}

	// Get candles from database
	candles, err := c.db.GetCandles(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to get candles for Bollinger Bands: %w", err)
	}

	if len(candles) < period {
		return 0, 0, 0, fmt.Errorf("insufficient candles for Bollinger Bands calculation: need %d, got %d",
			period, len(candles))
	}

	// Convert to closes array
	closes := make([]float64, len(candles))
	for i, candle := range candles {
		closes[i] = candle.ClosePrice
	}

	// Create Bollinger Bands instance using indicator v2
	bb := volatility.NewBollingerBandsWithPeriod[float64](period)

	// Create input channel and send closing prices
	inputChan := make(chan float64, len(closes))
	for _, price := range closes {
		inputChan <- price
	}
	close(inputChan)

	// Compute Bollinger Bands values using channels (returns 3 channels: upper, middle, lower)
	upperChan, middleChan, lowerChan := bb.Compute(inputChan)

	// Collect results from all three channels
	var upperResults, middleResults, lowerResults []float64

	// Read from upper channel
	for upperVal := range upperChan {
		upperResults = append(upperResults, upperVal)
	}
	// Read from middle channel
	for middleVal := range middleChan {
		middleResults = append(middleResults, middleVal)
	}
	// Read from lower channel
	for lowerVal := range lowerChan {
		lowerResults = append(lowerResults, lowerVal)
	}

	if len(upperResults) == 0 || len(middleResults) == 0 || len(lowerResults) == 0 {
		return 0, 0, 0, fmt.Errorf("Bollinger Bands calculation returned no values")
	}

	// Get latest values from each band
	latestUpper := upperResults[len(upperResults)-1]
	latestMiddle := middleResults[len(middleResults)-1]
	latestLower := lowerResults[len(lowerResults)-1]

	logger.Debugf("Calculated Bollinger Bands: Upper=%.4f, Middle=%.4f, Lower=%.4f",
		latestUpper, latestMiddle, latestLower)
	return latestUpper, latestMiddle, latestLower, nil
}

// CalculateSMA calculates Simple Moving Average manually
func (c *Calculator) CalculateSMA(pair, timeframe string, period int) (float64, error) {
	logger.Debugf("Calculating SMA for %s/%s with period %d", pair, timeframe, period)

	// Ensure we have enough candles
	requiredCandles := period * 2
	err := c.collector.EnsureCandlesAvailable(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, fmt.Errorf("failed to ensure candles availability: %w", err)
	}

	// Get candles from database
	candles, err := c.db.GetCandles(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, fmt.Errorf("failed to get candles for SMA: %w", err)
	}

	if len(candles) < period {
		return 0, fmt.Errorf("insufficient candles for SMA calculation: need %d, got %d", period, len(candles))
	}

	// Convert all candles to closes for SMA calculation

	// Calculate SMA using indicator v2 library with channels
	sma := trend.NewSmaWithPeriod[float64](period)

	// Create input channel and send closing prices
	inputChan := make(chan float64, len(candles))
	for _, candle := range candles {
		inputChan <- candle.ClosePrice
	}
	close(inputChan)

	// Compute SMA values using channels
	smaChan := sma.Compute(inputChan)

	// Collect all SMA values
	var smaValues []float64
	for value := range smaChan {
		smaValues = append(smaValues, value)
	}

	if len(smaValues) == 0 {
		return 0, fmt.Errorf("SMA calculation returned no values")
	}

	// Return latest SMA value
	latestSMA := smaValues[len(smaValues)-1]
	logger.Debugf("Calculated SMA: %.4f", latestSMA)

	return latestSMA, nil
}

// CalculateEMA calculates Exponential Moving Average using indicator v2 library
func (c *Calculator) CalculateEMA(pair, timeframe string, period int) (float64, error) {
	logger.Debugf("Calculating EMA for %s/%s with period %d", pair, timeframe, period)

	// Ensure we have enough candles
	requiredCandles := period * 3
	err := c.collector.EnsureCandlesAvailable(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, fmt.Errorf("failed to ensure candles availability: %w", err)
	}

	// Get candles from database
	candles, err := c.db.GetCandles(pair, timeframe, requiredCandles)
	if err != nil {
		return 0, fmt.Errorf("failed to get candles for EMA: %w", err)
	}

	if len(candles) < period {
		return 0, fmt.Errorf("insufficient candles for EMA calculation: need %d, got %d", period, len(candles))
	}

	// Create EMA instance using indicator v2
	ema := trend.NewEmaWithPeriod[float64](period)

	// Create input channel and send closing prices
	inputChan := make(chan float64, len(candles))
	for _, candle := range candles {
		inputChan <- candle.ClosePrice
	}
	close(inputChan)

	// Compute EMA values using channels
	emaChan := ema.Compute(inputChan)

	// Collect all EMA values
	var emaValues []float64
	for value := range emaChan {
		emaValues = append(emaValues, value)
	}

	if len(emaValues) == 0 {
		return 0, fmt.Errorf("EMA calculation returned no values")
	}

	// Return latest EMA value
	latestEMA := emaValues[len(emaValues)-1]
	logger.Debugf("Calculated EMA: %.4f", latestEMA)

	return latestEMA, nil
}

// GetCurrentPrice returns the latest close price from cached data
func (c *Calculator) GetCurrentPrice(pair, timeframe string) (float64, error) {
	// Get the most recent candle
	candles, err := c.db.GetCandles(pair, timeframe, 1)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest candle: %w", err)
	}

	if len(candles) == 0 {
		return 0, fmt.Errorf("no candles available for %s/%s", pair, timeframe)
	}

	return candles[len(candles)-1].ClosePrice, nil
}

// ValidateIndicatorParams validates common indicator parameters
func (c *Calculator) ValidateIndicatorParams(period int, pair, timeframe string) error {
	if period <= 0 {
		return fmt.Errorf("period must be positive, got %d", period)
	}
	if pair == "" {
		return fmt.Errorf("pair cannot be empty")
	}
	if timeframe == "" {
		return fmt.Errorf("timeframe cannot be empty")
	}
	return nil
}

// GetIndicatorStats returns statistics about indicator calculations
func (c *Calculator) GetIndicatorStats() map[string]interface{} {
	stats := make(map[string]interface{})

	// TODO: Add indicator calculation statistics
	stats["status"] = "active"
	stats["library"] = "github.com/cinar/indicator"

	return stats
}

// Note: CalculateVolatilityLegacy removed - use CalculateVolatility with standard deviation
