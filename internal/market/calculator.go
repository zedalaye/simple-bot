package market

import (
	"bot/internal/core/database"
	"bot/internal/logger"
	"fmt"
	"math"

	"github.com/cinar/indicator/v2/momentum"
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

	// Calculate volatility using manual standard deviation (indicator v2 std API to be investigated)
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

// CalculateMACD - Temporarily disabled until we confirm v2 API for MACD
func (c *Calculator) CalculateMACD(pair, timeframe string, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, histogram float64, err error) {
	logger.Debugf("MACD calculation temporarily disabled - focusing on RSI v2 implementation first")
	return 0, 0, 0, fmt.Errorf("MACD calculation temporarily disabled")
}

// CalculateBollingerBands - Temporarily disabled until we confirm v2 API
func (c *Calculator) CalculateBollingerBands(pair, timeframe string, period int, k float64) (upper, middle, lower float64, err error) {
	logger.Debugf("Bollinger Bands calculation temporarily disabled - focusing on RSI v2 implementation first")
	return 0, 0, 0, fmt.Errorf("Bollinger Bands calculation temporarily disabled")
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

	// Calculate simple moving average manually (v2 SMA API to be investigated)
	// Use the most recent 'period' candles
	if len(candles) < period {
		return 0, fmt.Errorf("insufficient candles for SMA calculation")
	}

	recentCandles := candles[len(candles)-period:]

	var sum float64
	for _, candle := range recentCandles {
		sum += candle.ClosePrice
	}

	latestSMA := sum / float64(period)
	logger.Debugf("Calculated SMA: %.4f", latestSMA)

	return latestSMA, nil
}

// CalculateEMA - Temporarily disabled until we confirm v2 API
func (c *Calculator) CalculateEMA(pair, timeframe string, period int) (float64, error) {
	logger.Debugf("EMA calculation temporarily disabled - focusing on RSI v2 implementation first")
	return 0, fmt.Errorf("EMA calculation temporarily disabled")
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

// Legacy method for backward compatibility - calculates volatility using manual method
func (c *Calculator) CalculateVolatilityLegacy(pair, timeframe string, period int) (float64, error) {
	// Get candles
	candles, err := c.db.GetCandles(pair, timeframe, period)
	if err != nil {
		return 0, fmt.Errorf("failed to get candles for volatility: %w", err)
	}

	if len(candles) < 2 {
		return 0, fmt.Errorf("not enough candles for volatility calculation")
	}

	// Extract prices
	prices := make([]float64, len(candles))
	for i, candle := range candles {
		prices[i] = candle.ClosePrice
	}

	// Calculate returns
	returns := make([]float64, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		returns[i-1] = (prices[i] - prices[i-1]) / prices[i-1]
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
	volatility := math.Sqrt(variance) * 100
	return volatility, nil
}
