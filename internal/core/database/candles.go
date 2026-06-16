package database

import (
	"bot/internal/logger"
	"database/sql"
	"fmt"
	"time"
)

// SaveCandle saves a candle to the database
func (db *DB) SaveCandle(pair, timeframe string, timestamp int64, open, high, low, close, volume float64) error {
	query := `INSERT OR IGNORE INTO candles (pair, timeframe, timestamp, open_price, high_price, low_price, close_price, volume) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.conn.Exec(query, pair, timeframe, timestamp, open, high, low, close, volume)
	if err != nil {
		return fmt.Errorf("failed to save candle: %w", err)
	}
	return nil
}

// GetCandles retrieves candles for a specific pair and timeframe
func (db *DB) GetCandles(pair, timeframe string, limit int) ([]Candle, error) {
	query := `
		SELECT id, pair, timeframe, timestamp, open_price, high_price, low_price, close_price, volume, created_at
		FROM candles
		WHERE pair = ? AND timeframe = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`
	rows, err := db.conn.Query(query, pair, timeframe, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get candles: %w", err)
	}
	defer rows.Close()

	var candles []Candle
	for rows.Next() {
		var candle Candle
		err := rows.Scan(&candle.ID, &candle.Pair, &candle.Timeframe, &candle.Timestamp,
			&candle.OpenPrice, &candle.HighPrice, &candle.LowPrice, &candle.ClosePrice,
			&candle.Volume, &candle.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan candle: %w", err)
		}
		candles = append(candles, candle)
	}

	// Reverse to get chronological order (oldest first)
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	return candles, nil
}

// GetLastCandle retrieves the most recent candle for a specific pair and timeframe
func (db *DB) GetLastCandle(pair, timeframe string) (*Candle, error) {
	query := `
		SELECT id, pair, timeframe, timestamp, open_price, high_price, low_price, close_price, volume, created_at
		FROM candles
		WHERE pair = ? AND timeframe = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`
	row := db.conn.QueryRow(query, pair, timeframe)

	var candle Candle
	err := row.Scan(&candle.ID, &candle.Pair, &candle.Timeframe, &candle.Timestamp,
		&candle.OpenPrice, &candle.HighPrice, &candle.LowPrice, &candle.ClosePrice,
		&candle.Volume, &candle.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No candles found
		}
		return nil, fmt.Errorf("failed to get last candle: %w", err)
	}

	return &candle, nil
}

// GetPairs retrieves the distinct list of pairs with candles
func (db *DB) GetPairs() ([]string, error) {
	query := `
	  SELECT DISTINCT pair FROM candles ORDER by 1
	`

	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get pairs: %w", err)
	}
	defer rows.Close()

	var pairs []string
	for rows.Next() {
		var pair string
		err := rows.Scan(&pair)
		if err != nil {
			return nil, fmt.Errorf("failed to scan candle: %w", err)
		}
		pairs = append(pairs, pair)
	}

	return pairs, nil
}

// GetCandleTimeframes retourne les timeframes distinctes disponibles en base
// pour une paire (utilisé par le backtest pour charger toutes les séries).
func (db *DB) GetCandleTimeframes(pair string) ([]string, error) {
	rows, err := db.conn.Query(`SELECT DISTINCT timeframe FROM candles WHERE pair = ?`, pair)
	if err != nil {
		return nil, fmt.Errorf("failed to get candle timeframes: %w", err)
	}
	defer rows.Close()

	var tfs []string
	for rows.Next() {
		var tf string
		if err := rows.Scan(&tf); err != nil {
			return nil, fmt.Errorf("failed to scan timeframe: %w", err)
		}
		tfs = append(tfs, tf)
	}
	return tfs, nil
}

// GetAllCandles retourne TOUTES les bougies d'une paire/timeframe en ordre
// chronologique (plus ancienne d'abord). Réservé aux usages hors temps réel
// (backtest, analyse) où l'on veut l'historique complet.
func (db *DB) GetAllCandles(pair, timeframe string) ([]Candle, error) {
	query := `
		SELECT id, pair, timeframe, timestamp, open_price, high_price, low_price, close_price, volume, created_at
		FROM candles
		WHERE pair = ? AND timeframe = ?
		ORDER BY timestamp ASC
	`
	rows, err := db.conn.Query(query, pair, timeframe)
	if err != nil {
		return nil, fmt.Errorf("failed to get all candles: %w", err)
	}
	defer rows.Close()

	var candles []Candle
	for rows.Next() {
		var c Candle
		if err := rows.Scan(&c.ID, &c.Pair, &c.Timeframe, &c.Timestamp,
			&c.OpenPrice, &c.HighPrice, &c.LowPrice, &c.ClosePrice, &c.Volume, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan candle: %w", err)
		}
		candles = append(candles, c)
	}
	return candles, nil
}

// GetActiveTimeframes retrieves active timeframes for a specific pair
func (db *DB) GetActiveTimeframes(pair string) ([]ActiveTimeframe, error) {
	query := `
		SELECT DISTINCT pair, s.rsi_timeframe as timeframe
		FROM strategies s
		WHERE s.pair = ? AND s.enabled = 1 AND s.rsi_timeframe IS NOT NULL
		UNION
		SELECT DISTINCT pair, s.volatility_timeframe as timeframe
		FROM strategies s
		WHERE s.pair = ? AND s.enabled = 1 AND s.volatility_timeframe IS NOT NULL
		UNION
		SELECT DISTINCT pair, s.macd_timeframe as timeframe
		FROM strategies s
		WHERE s.pair = ? AND s.enabled = 1 AND s.macd_timeframe IS NOT NULL
		UNION
		SELECT DISTINCT pair, s.bb_timeframe as timeframe
		FROM strategies s
		WHERE s.pair = ? AND s.enabled = 1 AND s.bb_timeframe IS NOT NULL
	`
	rows, err := db.conn.Query(query, pair, pair, pair, pair)
	if err != nil {
		return nil, fmt.Errorf("failed to get active timeframes: %w", err)
	}
	defer rows.Close()

	var timeframes []ActiveTimeframe
	for rows.Next() {
		var tf ActiveTimeframe
		err := rows.Scan(&tf.Pair, &tf.Timeframe)
		if err != nil {
			return nil, fmt.Errorf("failed to scan timeframe: %w", err)
		}
		timeframes = append(timeframes, tf)
	}

	return timeframes, nil
}

// CleanupOldCandles removes old candles older than specified days
func (db *DB) CleanupOldCandles(olderThanDays int) error {
	// Les timestamps des bougies sont en millisecondes (cohérent avec ccxt)
	cutoffTimestamp := time.Now().AddDate(0, 0, -olderThanDays).UnixMilli()
	query := `DELETE FROM candles WHERE timestamp < ?`
	result, err := db.conn.Exec(query, cutoffTimestamp)
	if err != nil {
		return fmt.Errorf("failed to cleanup old candles: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		logger.Infof("Cleaned up %d old candles", rowsAffected)
	}

	return nil
}
