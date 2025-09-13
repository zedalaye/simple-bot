package database

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateCycle creates a new cycle in the database
func (db *DB) CreateCycle(buyOrderId int) (*CycleEnhanced, error) {
	query := `INSERT INTO cycles (buy_order_id) VALUES (?)`
	result, err := db.conn.Exec(query, buyOrderId)
	if err != nil {
		return nil, fmt.Errorf("failed to create cycle: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return db.GetCycle(int(id))
}

// GetCycle retrieves a cycle by ID
func (db *DB) GetCycle(id int) (*CycleEnhanced, error) {
	query := `
		SELECT
			c.id, c.target_price, c.max_price, c.created_at, c.updated_at,
			bo.id, bo.strategy_id, bo.external_id, bo.side, bo.amount, bo.price, bo.fees, bo.status, bo.created_at, bo.updated_at,
			so.id, so.strategy_id, so.external_id, so.side, so.amount, so.price, so.fees, so.status, so.created_at, so.updated_at
		FROM cycles c
		JOIN orders bo ON c.buy_order_id = bo.id
		LEFT JOIN orders so ON c.sell_order_id = so.id
		WHERE c.id = ?
	`

	rows, err := db.conn.Query(query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get cycle: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("cycle not found")
	}

	scanResult, err := db.scanCycleRow(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to scan cycle row: %w", err)
	}

	// Construire les orders
	buyOrder := db.buildOrderFromScan(scanResult.BuyOrder)
	sellOrder := db.buildOrderFromScan(scanResult.SellOrder)

	// Construire le cycle enhanced
	cycle, err := db.buildCycleEnhancedFromOrders(
		scanResult.ID,
		scanResult.TargetPrice,
		scanResult.MaxPrice,
		scanResult.CreatedAt,
		scanResult.UpdatedAt,
		buyOrder,
		sellOrder,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build cycle enhanced: %w", err)
	}

	return cycle, nil
}

// GetOpenCycles retrieves all open cycles
func (db *DB) GetOpenCycles() ([]CycleEnhanced, error) {
	query := `
		SELECT
			c.id, c.target_price, c.max_price, c.created_at, c.updated_at,
			bo.id, bo.strategy_id, bo.external_id, bo.side, bo.amount, bo.price, bo.fees, bo.status, bo.created_at, bo.updated_at,
			so.id, so.strategy_id, so.external_id, so.side, so.amount, so.price, so.fees, so.status, so.created_at, so.updated_at
		FROM cycles c
		JOIN orders bo ON c.buy_order_id = bo.id
		LEFT JOIN orders so ON c.sell_order_id = so.id
		WHERE (bo.status = 'FILLED')
		  AND (c.sell_order_id IS NULL OR (so.status = 'CANCELLED'))
		ORDER BY c.created_at DESC
	`

	return db.executeCycleQuery(query)
}

// GetAllCycles retrieves all cycles
func (db *DB) GetAllCycles() ([]CycleEnhanced, error) {
	query := `
		SELECT
			c.id, c.target_price, c.max_price, c.created_at, c.updated_at,
			bo.id, bo.strategy_id, bo.external_id, bo.side, bo.amount, bo.price, bo.fees, bo.status, bo.created_at, bo.updated_at,
			so.id, so.strategy_id, so.external_id, so.side, so.amount, so.price, so.fees, so.status, so.created_at, so.updated_at
		FROM cycles c
		JOIN orders bo ON c.buy_order_id = bo.id
		LEFT JOIN orders so ON c.sell_order_id = so.id
		ORDER BY c.created_at DESC
	`

	return db.executeCycleQuery(query)
}

// GetCycleForBuyOrder retrieves a cycle by its buy order ID
func (db *DB) GetCycleForBuyOrder(buyOrderId int) (*CycleEnhanced, error) {
	var cycleId sql.NullInt64
	err := db.conn.QueryRow(`SELECT id FROM cycles WHERE buy_order_id = ?`, buyOrderId).Scan(&cycleId)
	if err != nil {
		return nil, fmt.Errorf("failed to get cycle id for buy order: %w", err)
	}
	if cycleId.Valid {
		return db.GetCycle(int(cycleId.Int64))
	} else {
		return nil, fmt.Errorf("db.GetCycleForBuyOrder() failed to get cycle id for buy order: %w", err)
	}
}

// GetCycleForSellOrder retrieves a cycle by its sell order ID
func (db *DB) GetCycleForSellOrder(sellOrderId int) (*CycleEnhanced, error) {
	var cycleId sql.NullInt64
	err := db.conn.QueryRow(`SELECT id FROM cycles WHERE sell_order_id = ?`, sellOrderId).Scan(&cycleId)
	if err != nil {
		return nil, fmt.Errorf("db.GetCycleForSellOrder() failed to get cycle id for buy order: %w", err)
	}
	if cycleId.Valid {
		return db.GetCycle(int(cycleId.Int64))
	} else {
		return nil, fmt.Errorf("db.GetCycleForSellOrder() failed to get cycle id for buy order: %w", err)
	}
}

// UpdateCycleMaxPrice updates the maximum price of a cycle
func (db *DB) UpdateCycleMaxPrice(id int, maxPrice float64) error {
	query := `UPDATE cycles SET max_price = ? WHERE id = ?`
	_, err := db.conn.Exec(query, maxPrice, id)
	if err != nil {
		return fmt.Errorf("failed to update max_price for cycle %d: %v", id, err)
	}
	return nil
}

// UpdateCycleTargetPrice updates the target price of a cycle
func (db *DB) UpdateCycleTargetPrice(id int, targetPrice float64) error {
	query := `UPDATE cycles SET target_price = ? WHERE id = ?`
	_, err := db.conn.Exec(query, targetPrice, id)
	if err != nil {
		return fmt.Errorf("failed to update target_price for cycle %d: %v", id, err)
	}
	return nil
}

// UpdateCycleSellOrder updates the sell order ID of a cycle
func (db *DB) UpdateCycleSellOrder(id int, sellOrderId int) error {
	query := `
		UPDATE cycles SET 
			sell_order_id = ?, 
			updated_at = CURRENT_TIMESTAMP 
		WHERE id = ?
	`
	_, err := db.conn.Exec(query, sellOrderId, id)
	if err != nil {
		return fmt.Errorf("failed to update cycle sell order: %w", err)
	}
	return nil
}

// UpdateCycleSellOrderForBuyOrder updates the sell order ID for a cycle by its buy order ID
func (db *DB) UpdateCycleSellOrderForBuyOrder(buyOrderId int, sellOrderId int) error {
	query := `
		UPDATE cycles SET 
			sell_order_id = ?, 
			updated_at = CURRENT_TIMESTAMP 
		WHERE buy_order_id = ?
	`
	_, err := db.conn.Exec(query, sellOrderId, buyOrderId)
	if err != nil {
		return fmt.Errorf("failed to update cycle sell order: %w", err)
	}
	return nil
}

// ForceCycleTimestamps forces the timestamps of a cycle
func (db *DB) ForceCycleTimestamps(id int, createdAt time.Time, updatedAt time.Time) error {
	query := `
		UPDATE cycles SET 
			created_at = ?,
			updated_at = ?
		WHERE id = ?
	`
	_, err := db.conn.Exec(query, createdAt, updatedAt, id)
	if err != nil {
		return fmt.Errorf("failed to force cycle timestamps: %w", err)
	}
	return nil
}

// GetOpenCyclesForStrategy retrieves open cycles for a specific strategy
func (db *DB) GetOpenCyclesForStrategy(strategyId int) ([]CycleEnhanced, error) {
	query := `
		SELECT
			c.id, c.target_price, c.max_price, c.created_at, c.updated_at,
			bo.id, bo.strategy_id, bo.external_id, bo.side, bo.amount, bo.price, bo.fees, bo.status, bo.created_at, bo.updated_at,
			so.id, so.strategy_id, so.external_id, so.side, so.amount, so.price, so.fees, so.status, so.created_at, so.updated_at
		FROM cycles c
		JOIN orders bo ON c.buy_order_id = bo.id
		LEFT JOIN orders so ON c.sell_order_id = so.id
		WHERE bo.strategy_id = ? AND (c.sell_order_id IS NULL OR so.status = 'CANCELLED')
		ORDER BY c.created_at DESC
	`

	return db.executeCycleQuery(query, strategyId)
}
