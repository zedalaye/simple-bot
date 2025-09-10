package market

import (
	"bot/internal/core/database"
	"bot/internal/logger"
	"fmt"
	"math"
	// Temporary manual calculations until indicator library issue is resolved
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

	// Calculate RSI manually (temporary implementation)
	if len(closes) < period+1 {
		return 0, fmt.Errorf("insufficient data for RSI calculation")
	}

	// Calculate gains and losses
	gains := make([]float64, len(closes)-1)
	losses := make([]float64, len(closes)-1)

	for i := 1; i < len(closes); i++ {
		change := closes[i] - closes[i-1]
		if change > 0 {
			gains[i-1] = change
			losses[i-1] = 0
		} else {
			gains[i-1] = 0
			losses[i-1] = -change
		}
	}

	// Calculate initial averages
	if len(gains) < period {
		return 0, fmt.Errorf("insufficient gains data for RSI")
	}

	var avgGain, avgLoss float64
	for i := 0; i < period; i++ {
		avgGain += gains[i]
		avgLoss += losses[i]
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	// Calculate exponential averages for remaining data
	for i := period; i < len(gains); i++ {
		avgGain = (avgGain*float64(period-1) + gains[i]) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + losses[i]) / float64(period)
	}

	// Calculate RSI
	if avgLoss == 0 {
		return 100, nil
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	logger.Debugf("Calculated RSI: %.2f", rsi)
	return rsi, nil
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

	// Calculate volatility manually (temporary implementation)
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

// CalculateMACD computes MACD manually (temporary implementation)
func (c *Calculator) CalculateMACD(pair, timeframe string, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, histogram float64, err error) {
	logger.Debugf("MACD calculation temporarily disabled - will be implemented with working indicator library")
	return 0, 0, 0, fmt.Errorf("MACD calculation not yet implemented - waiting for indicator library integration")
}

// CalculateBollingerBands computes Bollinger Bands manually (temporary implementation)
func (c *Calculator) CalculateBollingerBands(pair, timeframe string, period int, k float64) (upper, middle, lower float64, err error) {
	logger.Debugf("Bollinger Bands calculation temporarily disabled - will be implemented with working indicator library")
	return 0, 0, 0, fmt.Errorf("Bollinger Bands calculation not yet implemented - waiting for indicator library integration")
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

	// Use the most recent 'period' candles
	recentCandles := candles[len(candles)-period:]

	// Calculate simple moving average
	var sum float64
	for _, candle := range recentCandles {
		sum += candle.ClosePrice
	}

	latestSMA := sum / float64(period)
	logger.Debugf("Calculated SMA: %.4f", latestSMA)

	return latestSMA, nil
}

// CalculateEMA calculates Exponential Moving Average manually
func (c *Calculator) CalculateEMA(pair, timeframe string, period int) (float64, error) {
	logger.Debugf("EMA calculation temporarily disabled - will be implemented with working indicator library")
	return 0, fmt.Errorf("EMA calculation not yet implemented - waiting for indicator library integration")
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
