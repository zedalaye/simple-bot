package database

import (
	"database/sql"
	"fmt"
	"time"
)

// colonnes SELECT communes à toutes les requêtes de stratégie
const strategyColumns = `
	id, name, description, enabled, algorithm_name, cron_expression, quote_amount, max_concurrent_cycles,
	rsi_threshold, rsi_period, rsi_timeframe, macd_fast_period, macd_slow_period, macd_signal_period, macd_timeframe,
	bb_period, bb_multiplier, bb_timeframe, profit_target, trailing_stop_delta, sell_offset,
	volatility_period, volatility_adjustment, volatility_timeframe,
	trend_filter_enabled, trend_filter_fast_period, trend_filter_slow_period, trend_filter_timeframe,
	last_executed_at, next_execution_at, created_at, updated_at`

// rowScanner est implémenté par *sql.Row et *sql.Rows
type rowScanner interface {
	Scan(dest ...any) error
}

// scanStrategy scanne une ligne SQL en Strategy, gère les champs nullables
func scanStrategy(row rowScanner) (Strategy, error) {
	var s Strategy
	var lastExecutedAt, nextExecutionAt sql.NullTime
	var rsiThreshold sql.NullFloat64
	var rsiPeriod sql.NullInt64
	var volatilityPeriod sql.NullInt64
	var volatilityAdjustment sql.NullFloat64
	var trendFilterFastPeriod, trendFilterSlowPeriod sql.NullInt64

	err := row.Scan(
		&s.ID, &s.Name, &s.Description, &s.Enabled,
		&s.AlgorithmName, &s.CronExpression, &s.QuoteAmount, &s.MaxConcurrentCycles,
		&rsiThreshold, &rsiPeriod, &s.RSITimeframe, &s.MACDFastPeriod, &s.MACDSlowPeriod,
		&s.MACDSignalPeriod, &s.MACDTimeframe, &s.BBPeriod, &s.BBMultiplier, &s.BBTimeframe,
		&s.ProfitTarget, &s.TrailingStopDelta, &s.SellOffset,
		&volatilityPeriod, &volatilityAdjustment, &s.VolatilityTimeframe,
		&s.TrendFilterEnabled, &trendFilterFastPeriod, &trendFilterSlowPeriod, &s.TrendFilterTimeframe,
		&lastExecutedAt, &nextExecutionAt, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return Strategy{}, err
	}

	if rsiThreshold.Valid {
		s.RSIThreshold = &rsiThreshold.Float64
	}
	if rsiPeriod.Valid {
		period := int(rsiPeriod.Int64)
		s.RSIPeriod = &period
	}
	if volatilityPeriod.Valid {
		period := int(volatilityPeriod.Int64)
		s.VolatilityPeriod = &period
	}
	if volatilityAdjustment.Valid {
		s.VolatilityAdjustment = &volatilityAdjustment.Float64
	}
	if trendFilterFastPeriod.Valid {
		period := int(trendFilterFastPeriod.Int64)
		s.TrendFilterFastPeriod = &period
	}
	if trendFilterSlowPeriod.Valid {
		period := int(trendFilterSlowPeriod.Int64)
		s.TrendFilterSlowPeriod = &period
	}
	if lastExecutedAt.Valid {
		s.LastExecutedAt = &lastExecutedAt.Time
	}
	if nextExecutionAt.Valid {
		s.NextExecutionAt = &nextExecutionAt.Time
	}

	return s, nil
}

// queryStrategies exécute une requête et retourne la liste des stratégies scannées
func (db *DB) queryStrategies(query string, args ...any) ([]Strategy, error) {
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var strategies []Strategy
	for rows.Next() {
		s, err := scanStrategy(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan strategy: %w", err)
		}
		strategies = append(strategies, s)
	}
	return strategies, nil
}

// GetStrategy retrieves a strategy by ID
func (db *DB) GetStrategy(id int) (*Strategy, error) {
	query := `SELECT` + strategyColumns + ` FROM strategies WHERE id = ?`
	s, err := scanStrategy(db.conn.QueryRow(query, id))
	if err != nil {
		return nil, fmt.Errorf("failed to get strategy: %w", err)
	}
	return &s, nil
}

// GetEnabledStrategies retrieves all enabled strategies
func (db *DB) GetEnabledStrategies() ([]Strategy, error) {
	query := `SELECT` + strategyColumns + ` FROM strategies WHERE enabled = 1 ORDER BY created_at DESC`
	strategies, err := db.queryStrategies(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled strategies: %w", err)
	}
	return strategies, nil
}

// GetAllStrategies retrieves all strategies
func (db *DB) GetAllStrategies() ([]Strategy, error) {
	query := `SELECT` + strategyColumns + ` FROM strategies ORDER BY created_at DESC`
	strategies, err := db.queryStrategies(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all strategies: %w", err)
	}
	return strategies, nil
}

// UpdateStrategyExecution updates the last execution time of a strategy
func (db *DB) UpdateStrategyExecution(id int, lastExecuted time.Time) error {
	query := `UPDATE strategies SET last_executed_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := db.conn.Exec(query, lastExecuted, id)
	if err != nil {
		return fmt.Errorf("failed to update strategy execution time: %w", err)
	}
	return nil
}

// UpdateStrategyNextExecution updates the next execution time of a strategy
func (db *DB) UpdateStrategyNextExecution(strategyId int, nextExecution time.Time) error {
	query := `UPDATE strategies SET next_execution_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := db.conn.Exec(query, nextExecution, strategyId)
	if err != nil {
		return fmt.Errorf("failed to update strategy next execution: %w", err)
	}
	return nil
}

// CountActiveOrdersForStrategy counts active orders for a specific strategy
func (db *DB) CountActiveOrdersForStrategy(strategyId int) (int, error) {
	query := `SELECT COUNT(*) FROM orders WHERE strategy_id = ? AND status = ?`
	var count int
	err := db.conn.QueryRow(query, strategyId, Pending).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count active orders for strategy: %w", err)
	}
	return count, nil
}

// CountOrdersForStrategy counts all orders for a specific strategy
func (db *DB) CountOrdersForStrategy(strategyId int) (int, error) {
	query := `SELECT COUNT(*) FROM orders WHERE strategy_id = ?`
	var count int
	err := db.conn.QueryRow(query, strategyId).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count orders for strategy: %w", err)
	}
	return count, nil
}

// GetStrategyStats retrieves statistics for a specific strategy
func (db *DB) GetStrategyStats(strategyId int) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Count orders by status for this strategy
	query := `
		SELECT
			COUNT(CASE WHEN status = 'PENDING' THEN 1 END) as pending,
			COUNT(CASE WHEN status = 'FILLED' THEN 1 END) as filled,
			COUNT(CASE WHEN status = 'CANCELLED' THEN 1 END) as cancelled
		FROM orders
		WHERE strategy_id = ?
	`
	var pending, filled, cancelled int
	err := db.conn.QueryRow(query, strategyId).Scan(&pending, &filled, &cancelled)
	if err != nil {
		return nil, fmt.Errorf("failed to get strategy order stats: %w", err)
	}

	stats["pending_orders"] = pending
	stats["filled_orders"] = filled
	stats["cancelled_orders"] = cancelled

	// Count active cycles for this strategy
	var activeCyclesCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM cycles c JOIN orders bo ON c.buy_order_id = bo.id WHERE bo.strategy_id = ? AND c.sell_order_id IS NULL`, strategyId).Scan(&activeCyclesCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get strategy active cycles count: %w", err)
	}
	stats["active_cycles"] = activeCyclesCount

	return stats, nil
}

// CreateExampleStrategy creates an example strategy (for command line tools)
func (db *DB) CreateExampleStrategy(name, description, algorithm, cron string, amount, profitTarget float64, rsiThresh *float64, rsiPeriod *int) error {
	// Check if strategy exists
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM strategies WHERE name = ?`, name).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check strategy existence: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("strategy %s already exists", name)
	}

	// Insert strategy with hardcoded defaults (for cmd tool compatibility)
	query := `
		INSERT INTO strategies (
			name, description, enabled, algorithm_name, cron_expression, quote_amount,
			rsi_threshold, rsi_period, rsi_timeframe, profit_target, trailing_stop_delta, sell_offset,
			volatility_period, volatility_adjustment, volatility_timeframe, max_concurrent_cycles
		) VALUES (?, ?, 1, ?, ?, ?, ?, ?, '4h', ?, 0.1, 0.1, 7, 50.0, '4h', 1)
	`

	_, err = db.conn.Exec(query, name, description, algorithm, cron, amount,
		rsiThresh, rsiPeriod, profitTarget)
	if err != nil {
		return fmt.Errorf("failed to create strategy: %w", err)
	}

	return nil
}

// CreateStrategyFromWeb creates a strategy from web interface with full parameters
func (db *DB) CreateStrategyFromWeb(name, description, algorithm, cron string, enabled bool,
	quoteAmount, profitTarget, trailingStopDelta, sellOffset float64,
	rsiThreshold *float64, rsiPeriod *int, rsiTimeframe string,
	macdFastPeriod, macdSlowPeriod, macdSignalPeriod int, macdTimeframe string,
	bbPeriod int, bbMultiplier float64, bbTimeframe string,
	volatilityPeriod *int, volatilityAdjustment *float64, volatilityTimeframe string,
	trendFilterEnabled bool, trendFilterFastPeriod *int, trendFilterSlowPeriod *int, trendFilterTimeframe string,
	concurrentCycles int) error {

	// Check if strategy exists
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM strategies WHERE name = ?`, name).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check strategy existence: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("strategy %s already exists", name)
	}

	// Set defaults for timeframes if empty
	if rsiTimeframe == "" {
		rsiTimeframe = "4h"
	}
	if macdTimeframe == "" {
		macdTimeframe = "4h"
	}
	if bbTimeframe == "" {
		bbTimeframe = "1h"
	}
	if volatilityTimeframe == "" {
		volatilityTimeframe = "4h"
	}
	if trendFilterTimeframe == "" {
		trendFilterTimeframe = "1d"
	}

	// Insert strategy with all web form parameters
	query := `
		INSERT INTO strategies (
			name, description, enabled, algorithm_name, cron_expression, quote_amount,
			rsi_threshold, rsi_period, rsi_timeframe,
			macd_fast_period, macd_slow_period, macd_signal_period, macd_timeframe,
			bb_period, bb_multiplier, bb_timeframe,
			profit_target, trailing_stop_delta, sell_offset,
			volatility_period, volatility_adjustment, volatility_timeframe,
			trend_filter_enabled, trend_filter_fast_period, trend_filter_slow_period, trend_filter_timeframe,
			max_concurrent_cycles
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = db.conn.Exec(query, name, description, enabled, algorithm, cron, quoteAmount,
		rsiThreshold, rsiPeriod, rsiTimeframe,
		macdFastPeriod, macdSlowPeriod, macdSignalPeriod, macdTimeframe,
		bbPeriod, bbMultiplier, bbTimeframe,
		profitTarget, trailingStopDelta, sellOffset,
		volatilityPeriod, volatilityAdjustment, volatilityTimeframe,
		trendFilterEnabled, trendFilterFastPeriod, trendFilterSlowPeriod, trendFilterTimeframe,
		concurrentCycles)
	if err != nil {
		return fmt.Errorf("failed to create strategy: %w", err)
	}

	return nil
}

// UpdateStrategy updates an existing strategy
func (db *DB) UpdateStrategy(id int, name, description, algorithm, cron string, enabled bool,
	quoteAmount, profitTarget, trailingStopDelta, sellOffset float64,
	rsiThreshold *float64, rsiPeriod *int, rsiTimeframe string,
	macdFastPeriod, macdSlowPeriod, macdSignalPeriod int, macdTimeframe string,
	bbPeriod int, bbMultiplier float64, bbTimeframe string,
	volatilityPeriod *int, volatilityAdjustment *float64, volatilityTimeframe string,
	trendFilterEnabled bool, trendFilterFastPeriod *int, trendFilterSlowPeriod *int, trendFilterTimeframe string,
	maxConcurrentCycles int) error {

	// Set defaults for timeframes if empty
	if rsiTimeframe == "" {
		rsiTimeframe = "4h"
	}
	if macdTimeframe == "" {
		macdTimeframe = "4h"
	}
	if bbTimeframe == "" {
		bbTimeframe = "1h"
	}
	if volatilityTimeframe == "" {
		volatilityTimeframe = "4h"
	}
	if trendFilterTimeframe == "" {
		trendFilterTimeframe = "1d"
	}

	query := `
		UPDATE strategies SET
      name = ?,
			description = ?,
			algorithm_name = ?,
			cron_expression = ?,
			enabled = ?,
      quote_amount = ?,
			profit_target = ?,
			trailing_stop_delta = ?,
			sell_offset = ?,
      rsi_threshold = ?,
			rsi_period = ?,
			rsi_timeframe = ?,
			macd_fast_period = ?,
			macd_slow_period = ?,
			macd_signal_period = ?,
			macd_timeframe = ?,
			bb_period = ?,
			bb_multiplier = ?,
			bb_timeframe = ?,
			volatility_period = ?,
			volatility_adjustment = ?,
			volatility_timeframe = ?,
			trend_filter_enabled = ?,
			trend_filter_fast_period = ?,
			trend_filter_slow_period = ?,
			trend_filter_timeframe = ?,
			max_concurrent_cycles = ?,
			updated_at = CURRENT_TIMESTAMP
    WHERE id = ?
	`

	_, err := db.conn.Exec(query, name, description, algorithm, cron, enabled,
		quoteAmount, profitTarget, trailingStopDelta, sellOffset,
		rsiThreshold, rsiPeriod, rsiTimeframe,
		macdFastPeriod, macdSlowPeriod, macdSignalPeriod, macdTimeframe,
		bbPeriod, bbMultiplier, bbTimeframe,
		volatilityPeriod, volatilityAdjustment, volatilityTimeframe,
		trendFilterEnabled, trendFilterFastPeriod, trendFilterSlowPeriod, trendFilterTimeframe,
		maxConcurrentCycles, id)

	return err
}

// ToggleStrategyEnabled toggles the enabled status of a strategy
func (db *DB) ToggleStrategyEnabled(id int) error {
	query := `UPDATE strategies SET enabled = NOT enabled, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := db.conn.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to toggle strategy enabled status: %w", err)
	}
	return nil
}

// DeleteStrategy deletes a strategy if it has no associated orders
func (db *DB) DeleteStrategy(id int) error {
	ordersCount, err := db.CountOrdersForStrategy(id)
	if err != nil {
		return fmt.Errorf("failed to check orders for strategy: %w", err)
	}
	if ordersCount > 0 {
		return fmt.Errorf("cannot delete strategy: %d orders are associated with this strategy", ordersCount)
	}

	// Safe to delete
	query := `DELETE FROM strategies WHERE id = ?`
	result, err := db.conn.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete strategy: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("strategy not found")
	}

	return nil
}

// DeleteNonLegacyStrategies deletes all strategies except the legacy one (ID = 1)
func (db *DB) DeleteNonLegacyStrategies() (int64, error) {
	result, err := db.conn.Exec(`DELETE FROM strategies WHERE id != 1`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete strategies: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}
