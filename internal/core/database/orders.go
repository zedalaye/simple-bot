package database

import (
	"database/sql"
	"fmt"
	"time"
)

// CreateOrder creates a new order in the database
func (db *DB) CreateOrder(externalId string, side OrderSide, amount, price, fees float64, strategyId int) (*Order, error) {
	query := `
	  INSERT INTO orders (external_id, side, amount, price, fees, status, strategy_id) 
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	result, err := db.conn.Exec(query, externalId, side, amount, price, fees, Pending, strategyId)
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return db.GetOrder(int(id))
}

// GetOrder retrieves an order by ID
func (db *DB) GetOrder(id int) (*Order, error) {
	query := `
		SELECT id, strategy_id, external_id, side, amount, price, fees, status, created_at, updated_at
		FROM orders
		WHERE id = ?
	`
	row := db.conn.QueryRow(query, id)

	var order Order
	var strategyId sql.NullInt64
	err := row.Scan(&order.ID, &strategyId, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
		&order.Status, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("db.GetOrder() failed to scan order row: %w", err)
	}

	if strategyId.Valid {
		id := int(strategyId.Int64)
		order.StrategyID = &id
	}

	return &order, nil
}

// GetOrderByExternalID retrieves an order by external ID
func (db *DB) GetOrderByExternalID(externalId string) (*Order, error) {
	query := `
		SELECT id, external_id, side, amount, price, fees, status, strategy_id, created_at, updated_at 
		FROM orders 
		WHERE external_id = ?
	`
	row := db.conn.QueryRow(query, externalId)

	var order Order
	var strategyId sql.NullInt64
	err := row.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
		&order.Status, &strategyId, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get order by external id: %w", err)
	}

	if strategyId.Valid {
		id := int(strategyId.Int64)
		order.StrategyID = &id
	}

	return &order, nil
}

// GetPendingOrders retrieves all pending orders
func (db *DB) GetPendingOrders() ([]Order, error) {
	query := `
		SELECT id, external_id, side, amount, price, fees, status, strategy_id, created_at, updated_at
		FROM orders
		WHERE status = ?
		ORDER BY created_at ASC
	`
	return db.executeOrderQuery(query, Pending)
}

// UpdateOrderStatus updates the status of an order
func (db *DB) UpdateOrderStatus(externalId string, status OrderStatus) error {
	query := `
		UPDATE orders SET 
			status = ?, 
			updated_at = CURRENT_TIMESTAMP 
		WHERE external_id = ?
	`
	_, err := db.conn.Exec(query, status, externalId)
	if err != nil {
		return fmt.Errorf("failed to update order status: %w", err)
	}
	return nil
}

// GetOldOrders retrieves old orders (older than the specified time)
func (db *DB) GetOldOrders(olderThan time.Time) ([]Order, error) {
	query := `
		SELECT id, external_id, side, amount, price, fees, status, strategy_id, created_at, updated_at
		FROM orders
		WHERE status = ? AND created_at < ?
	`
	return db.executeOrderQuery(query, Pending, olderThan)
}

// GetAllOrders retrieves all orders (not just pending ones)
func (db *DB) GetAllOrders() ([]Order, error) {
	query := `
		SELECT id, external_id, side, amount, price, fees, status, strategy_id, created_at, updated_at
		FROM orders
		ORDER BY created_at DESC
	`
	return db.executeOrderQuery(query)
}

// GetOrdersWithPagination retrieves orders with pagination
func (db *DB) GetOrdersWithPagination(status OrderStatus, limit, offset int) ([]Order, int, error) {
	// Count total
	var total int
	countQuery := `SELECT COUNT(*) FROM orders`
	var countArgs []interface{}

	if status != "" {
		countQuery += ` WHERE status = ?`
		countArgs = append(countArgs, status)
	}

	err := db.conn.QueryRow(countQuery, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count orders: %w", err)
	}

	// Retrieve orders with the common function
	query := `
		SELECT id, external_id, side, amount, price, fees, status, strategy_id, created_at, updated_at
		FROM orders
	`

	var queryArgs []interface{}
	if status != "" {
		query += ` WHERE status = ?`
		queryArgs = append(queryArgs, status)
	}

	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	queryArgs = append(queryArgs, limit, offset)

	orders, err := db.executeOrderQuery(query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get orders with pagination: %w", err)
	}

	return orders, total, nil
}

// CountTodayBuyOrders counts the number of BUY orders created in the last 24 hours
func (db *DB) CountTodayBuyOrders() (int, error) {
	since := time.Now().AddDate(0, 0, -1)
	query := `
		SELECT COUNT(*) FROM orders
		WHERE side = ? AND created_at >= ?
	`
	row := db.conn.QueryRow(query, Buy, since)
	var count int
	err := row.Scan(&count)
	return count, err
}

// CleanupOldData removes old finished orders (older than X days)
func (db *DB) CleanupOldData(olderThanDays int) error {
	cutoffDate := time.Now().AddDate(0, 0, -olderThanDays)

	// Remove old FILLED or CANCELLED orders
	query := `DELETE FROM orders WHERE status IN (?, ?) AND updated_at < ?`
	result, err := db.conn.Exec(query, Filled, Cancelled, cutoffDate)
	if err != nil {
		return fmt.Errorf("failed to cleanup old orders: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		// Note: logger import would be needed here, but keeping it simple for now
		fmt.Printf("Cleaned up %d old orders\n", rowsAffected)
	}

	return nil
}
