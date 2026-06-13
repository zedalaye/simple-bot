package market

import (
	"bot/internal/core/database"
	"bot/internal/logger"
	"fmt"
	"strconv"
	"time"
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

// CollectCandles fetches the most recent candles for a specific pair/timeframe and stores them in DB.
//
// On récupère toujours les `limit` dernières bougies (sans `since`) : c'est ce que les exchanges
// renvoient nativement et cela garantit des données à jour pour le graphe et les indicateurs.
// Fetcher « depuis la dernière bougie connue » menait à un crawl avant inrattrapable dès qu'une
// timeframe prenait du retard (le graphe restait bloqué dans le passé). `INSERT OR IGNORE` rend
// l'opération idempotente ; l'historique s'étend naturellement vers l'avant au fil du temps.
func (mdc *MarketDataCollector) CollectCandles(pair, timeframe string, limit int) error {
	logger.Debugf("Collecting candles for %s/%s (limit: %d)", pair, timeframe, limit)

	// Récupérer les dernières bougies (pas de `since` : l'exchange renvoie les plus récentes)
	candles, err := mdc.exchange.FetchCandles(pair, timeframe, nil, int64(limit))
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

// EnsureCandlesAvailable makes sure we have enough AND fresh candles in DB for calculations.
//
// On vérifie le nombre de bougies ET leur fraîcheur : sans le contrôle de fraîcheur, une timeframe
// en retard était jugée « suffisante » et les indicateurs étaient calculés sur des bougies périmées.
func (mdc *MarketDataCollector) EnsureCandlesAvailable(pair, timeframe string, requiredCount int) error {
	// Check if we have enough candles
	candles, err := mdc.db.GetCandles(pair, timeframe, requiredCount)
	if err != nil {
		return fmt.Errorf("failed to get candles from DB: %w", err)
	}

	// Les bougies sont-elles à jour ? (GetCandles renvoie l'ordre chronologique, plus récent en dernier)
	fresh := false
	if len(candles) > 0 {
		lastTs := candles[len(candles)-1].Timestamp
		age := time.Since(time.UnixMilli(lastTs))
		fresh = age < 2*timeframeDuration(timeframe)
	}

	if len(candles) >= requiredCount && fresh {
		logger.Debugf("Sufficient & fresh candles available for %s/%s: %d >= %d", pair, timeframe, len(candles), requiredCount)
		return nil
	}

	logger.Infof("[%s] Candles for %s/%s insufficient or stale (have %d, need %d, fresh=%t), collecting...", mdc.exchangeName, pair, timeframe, len(candles), requiredCount, fresh)

	// We need more (or fresher) candles, fetch them
	return mdc.CollectCandles(pair, timeframe, requiredCount*2) // Fetch double to have some buffer
}

// timeframeDuration convertit une timeframe ("1m", "5m", "1h", "1d", "1w", "1M") en durée.
// Note : "m" = minute, "M" = mois (sensible à la casse).
func timeframeDuration(timeframe string) time.Duration {
	if len(timeframe) < 2 {
		return time.Minute
	}
	unit := timeframe[len(timeframe)-1]
	n, err := strconv.Atoi(timeframe[:len(timeframe)-1])
	if err != nil || n <= 0 {
		return time.Minute
	}
	switch unit {
	case 'm':
		return time.Duration(n) * time.Minute
	case 'h':
		return time.Duration(n) * time.Hour
	case 'd':
		return time.Duration(n) * 24 * time.Hour
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour
	case 'M':
		return time.Duration(n) * 30 * 24 * time.Hour
	default:
		return time.Minute
	}
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
