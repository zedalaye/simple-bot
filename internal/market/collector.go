package market

import (
	"bot/internal/core/database"
	"bot/internal/logger"
	"fmt"
)

// BotCandle represents a market data candle (same structure as bot.Candle)
type BotCandle struct {
	Timestamp int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

// Exchange interface for market data collection
type Exchange interface {
	FetchCandles(pair string, timeframe string, since *int64, limit int64) ([]BotCandle, error)
	GetPrice(pair string) (float64, error)
}

// MarketDataCollector manages candle data collection and storage
type MarketDataCollector struct {
	exchangeName string
	pair         string
	db           *database.DB
	exchange     Exchange
}

// NewMarketDataCollector creates a new market data collector
func NewMarketDataCollector(exchangeName, pair string, db *database.DB, exchange Exchange) *MarketDataCollector {
	return &MarketDataCollector{
		exchangeName: exchangeName,
		pair:         pair,
		db:           db,
		exchange:     exchange,
	}
}

// CollectCandles fetches missing candles for a specific pair/timeframe and stores them in DB
func (mdc *MarketDataCollector) CollectCandles(pair, timeframe string, limit int) error {
	logger.Debugf("Collecting candles for %s/%s (limit: %d)", pair, timeframe, limit)

	// 1. Get the last candle from DB to know where to start fetching
	lastCandle, err := mdc.db.GetLastCandle(pair, timeframe)
	if err != nil {
		return fmt.Errorf("failed to get last candle from DB: %w", err)
	}

	// 2. Calculate since timestamp for fetching
	var since *int64
	if lastCandle != nil {
		// Start from the next candle after the last one we have
		sinceTime := lastCandle.Timestamp + 1
		since = &sinceTime
		logger.Debugf("Last candle found at timestamp %d, fetching since %d", lastCandle.Timestamp, sinceTime)
	} else {
		logger.Debugf("No existing candles found, fetching complete history")
	}

	// 3. Fetch candles from exchange
	candles, err := mdc.exchange.FetchCandles(pair, timeframe, since, int64(limit))
	if err != nil {
		return fmt.Errorf("failed to fetch candles from exchange: %w", err)
	}

	if len(candles) == 0 {
		logger.Debugf("No new candles to collect for %s/%s", pair, timeframe)
		return nil
	}

	logger.Debugf("Fetched %d candles from exchange", len(candles))

	// 4. Save candles to database (INSERT OR IGNORE handles duplicates)
	saved := 0
	for _, candle := range candles {
		err := mdc.db.SaveCandle(pair, timeframe, candle.Timestamp,
			candle.Open, candle.High, candle.Low, candle.Close, candle.Volume)
		if err != nil {
			logger.Warnf("Failed to save candle %s/%s at %d: %v", pair, timeframe, candle.Timestamp, err)
			continue
		}
		saved++
	}

	logger.Infof("[%s] Saved %d/%d candles for %s/%s", mdc.exchangeName, saved, len(candles), pair, timeframe)
	return nil
}

// CollectAllActiveTimeframes collects candles for all timeframes used by enabled strategies
func (mdc *MarketDataCollector) CollectAllActiveTimeframes() error {
	logger.Debug("Collecting candles for all active timeframes...")

	// Get all active timeframes from enabled strategies
	activeTimeframes, err := mdc.db.GetActiveTimeframes(mdc.pair)
	if err != nil {
		return fmt.Errorf("failed to get active timeframes: %w", err)
	}

	if len(activeTimeframes) == 0 {
		logger.Debug("No active timeframes found")
		return nil
	}

	logger.Infof("[%s] Found %d active timeframes to collect", mdc.exchangeName, len(activeTimeframes))

	// Collect candles for each timeframe
	errors := 0
	for _, tf := range activeTimeframes {
		err := mdc.CollectCandles(tf.Pair, tf.Timeframe, 100) // Fetch up to 100 candles per timeframe
		if err != nil {
			logger.Errorf("Failed to collect candles for %s/%s: %v", tf.Pair, tf.Timeframe, err)
			errors++
			continue
		}
	}

	if errors > 0 {
		logger.Warnf("[%s] Collection completed with %d errors out of %d timeframes", mdc.exchangeName, errors, len(activeTimeframes))
	} else {
		logger.Infof("[%s] Successfully collected candles for all %d timeframes", mdc.exchangeName, len(activeTimeframes))
	}

	return nil
}

// GetCandlesFromDB retrieves candles from database (cached data)
func (mdc *MarketDataCollector) GetCandlesFromDB(pair, timeframe string, limit int) ([]database.Candle, error) {
	return mdc.db.GetCandles(pair, timeframe, limit)
}

// EnsureCandlesAvailable makes sure we have enough candles in DB for calculations
func (mdc *MarketDataCollector) EnsureCandlesAvailable(pair, timeframe string, requiredCount int) error {
	// Check if we have enough candles
	candles, err := mdc.db.GetCandles(pair, timeframe, requiredCount)
	if err != nil {
		return fmt.Errorf("failed to get candles from DB: %w", err)
	}

	if len(candles) >= requiredCount {
		logger.Debugf("Sufficient candles available for %s/%s: %d >= %d", pair, timeframe, len(candles), requiredCount)
		return nil
	}

	logger.Infof("[%s] Insufficient candles for %s/%s: %d < %d, collecting more...", mdc.exchangeName, pair, timeframe, len(candles), requiredCount)

	// We need more candles, fetch them
	return mdc.CollectCandles(pair, timeframe, requiredCount*2) // Fetch double to have some buffer
}

// CleanupOldCandles removes old candles to save storage space
func (mdc *MarketDataCollector) CleanupOldCandles(olderThanDays int) error {
	logger.Infof("[%s] Cleaning up candles older than %d days", mdc.exchangeName, olderThanDays)
	return mdc.db.CleanupOldCandles(olderThanDays)
}

// GetStats returns statistics about stored candles
func (mdc *MarketDataCollector) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// TODO: Add candle statistics queries to database
	// For now, return basic info
	stats["status"] = "active"

	return stats, nil
}
