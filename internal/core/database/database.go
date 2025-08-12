package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type OrderSide string

const (
	Buy  OrderSide = "BUY"
	Sell OrderSide = "SELL"
)

type OrderStatus string

const (
	Pending   OrderStatus = "PENDING"
	Filled    OrderStatus = "FILLED"
	Cancelled OrderStatus = "CANCELLED"
)

type Position struct {
	ID        int       `json:"id"`
	Price     float64   `json:"price"`
	Amount    float64   `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Order struct {
	ID         int         `json:"id"`
	ExternalID string      `json:"external_id"` // ID de l'exchange
	Side       OrderSide   `json:"side"`
	Amount     float64     `json:"amount"`
	Price      float64     `json:"price"`
	Status     OrderStatus `json:"status"`
	PositionID *int        `json:"position_id,omitempty"` // Pour lier les ordres de vente aux positions
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type DB struct {
	conn *sql.DB
}

func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.createTables(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) createTables() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS positions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			price REAL NOT NULL,
			amount REAL NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			external_id TEXT UNIQUE NOT NULL,
			side TEXT NOT NULL CHECK(side IN ('BUY', 'SELL')),
			amount REAL NOT NULL,
			price REAL NOT NULL,
			status TEXT NOT NULL CHECK(status IN ('PENDING', 'FILLED', 'CANCELLED')),
			position_id INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(position_id) REFERENCES positions(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_external_id ON orders(external_id)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status)`,
		`CREATE INDEX IF NOT EXISTS idx_positions_created_at ON positions(created_at)`,
	}

	for _, query := range queries {
		if _, err := db.conn.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query: %s, error: %w", query, err)
		}
	}

	return nil
}

// Positions
func (db *DB) CreatePosition(price, amount float64) (*Position, error) {
	query := `INSERT INTO positions (price, amount) VALUES (?, ?)`
	result, err := db.conn.Exec(query, price, amount)
	if err != nil {
		return nil, fmt.Errorf("failed to create position: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return db.GetPosition(int(id))
}

func (db *DB) GetPosition(id int) (*Position, error) {
	query := `SELECT id, price, amount, created_at, updated_at 
			  FROM positions 
		      WHERE id = ?`
	row := db.conn.QueryRow(query, id)

	var pos Position
	err := row.Scan(&pos.ID, &pos.Price, &pos.Amount, &pos.CreatedAt, &pos.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get position: %w", err)
	}

	return &pos, nil
}

func (db *DB) GetAllPositions() ([]Position, error) {
	query := `SELECT id, price, amount, created_at, updated_at 
		      FROM positions 
		      ORDER BY created_at DESC`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}
	defer rows.Close()

	var positions []Position
	for rows.Next() {
		var pos Position
		err := rows.Scan(&pos.ID, &pos.Price, &pos.Amount, &pos.CreatedAt, &pos.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan position: %w", err)
		}
		positions = append(positions, pos)
	}

	return positions, nil
}

func (db *DB) DeletePosition(id int) error {
	query := `DELETE FROM positions WHERE id = ?`
	_, err := db.conn.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete position: %w", err)
	}
	return nil
}

// Orders
func (db *DB) CreateOrder(externalID string, side OrderSide, amount, price float64, positionID *int) (*Order, error) {
	query := `INSERT INTO orders (external_id, side, amount, price, status, position_id) VALUES (?, ?, ?, ?, ?, ?)`
	result, err := db.conn.Exec(query, externalID, side, amount, price, Pending, positionID)
	if err != nil {
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return db.GetOrder(int(id))
}

func (db *DB) GetOrder(id int) (*Order, error) {
	query := `SELECT id, external_id, side, amount, price, status, position_id, created_at, updated_at 
		      FROM orders 
		      WHERE id = ?`
	row := db.conn.QueryRow(query, id)

	var order Order
	var positionID sql.NullInt64
	err := row.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Status, &positionID, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	if positionID.Valid {
		id := int(positionID.Int64)
		order.PositionID = &id
	}

	return &order, nil
}

func (db *DB) GetOrderByExternalID(externalID string) (*Order, error) {
	query := `SELECT id, external_id, side, amount, price, status, position_id, created_at, updated_at 
		      FROM orders 
		      WHERE external_id = ?`
	row := db.conn.QueryRow(query, externalID)

	var order Order
	var positionID sql.NullInt64
	err := row.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Status, &positionID, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get order by external id: %w", err)
	}

	if positionID.Valid {
		id := int(positionID.Int64)
		order.PositionID = &id
	}

	return &order, nil
}

func (db *DB) GetPendingOrders() ([]Order, error) {
	query := `SELECT id, external_id, side, amount, price, status, position_id, created_at, updated_at 
		      FROM orders 
		      WHERE status = ? 
		      ORDER BY created_at ASC`
	rows, err := db.conn.Query(query, Pending)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var order Order
		var positionID sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Status, &positionID, &order.CreatedAt, &order.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}

		if positionID.Valid {
			id := int(positionID.Int64)
			order.PositionID = &id
		}

		orders = append(orders, order)
	}

	return orders, nil
}

func (db *DB) UpdateOrderStatus(externalID string, status OrderStatus) error {
	query := `UPDATE orders SET 
			    status = ?, 
			    updated_at = CURRENT_TIMESTAMP 
		      WHERE external_id = ?`
	_, err := db.conn.Exec(query, status, externalID)
	if err != nil {
		return fmt.Errorf("failed to update order status: %w", err)
	}
	return nil
}

func (db *DB) GetOldOrders(olderThan time.Time) ([]Order, error) {
	query := `SELECT id, external_id, side, amount, price, status, position_id, created_at, updated_at 
		      FROM orders 
		      WHERE status = ? AND created_at < ?`
	rows, err := db.conn.Query(query, Pending, olderThan)
	if err != nil {
		return nil, fmt.Errorf("failed to get old orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var order Order
		var positionID sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Status, &positionID, &order.CreatedAt, &order.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}

		if positionID.Valid {
			id := int(positionID.Int64)
			order.PositionID = &id
		}

		orders = append(orders, order)
	}

	return orders, nil
}

// Méthodes utilitaires
func (db *DB) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Nombre de positions actives
	var posCount int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM positions`).Scan(&posCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get positions count: %w", err)
	}
	stats["active_positions"] = posCount

	// Nombre d'ordres en attente
	var pendingCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM orders WHERE status = ?`, Pending).Scan(&pendingCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending orders count: %w", err)
	}
	stats["pending_orders"] = pendingCount

	// Nombre d'ordres exécutés
	var filledCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM orders WHERE status = ?`, Filled).Scan(&filledCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get filled orders count: %w", err)
	}
	stats["filled_orders"] = filledCount

	// Nombre d'ordres annulés
	var cancelledCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM orders WHERE status = ?`, Cancelled).Scan(&cancelledCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get cancelled orders count: %w", err)
	}
	stats["cancelled_orders"] = cancelledCount

	// Valeur totale des positions (estimation)
	var totalValue sql.NullFloat64
	err = db.conn.QueryRow(`SELECT SUM(price * amount) FROM positions`).Scan(&totalValue)
	if err != nil {
		return nil, fmt.Errorf("failed to get total positions value: %w", err)
	}
	if totalValue.Valid {
		stats["total_positions_value"] = totalValue.Float64
	} else {
		stats["total_positions_value"] = 0.0
	}

	return stats, nil
}

// GetAllOrders récupère tous les ordres (pas seulement pending)
func (db *DB) GetAllOrders() ([]Order, error) {
	query := `SELECT id, external_id, side, amount, price, status, position_id, created_at, updated_at 
		      FROM orders 
		      ORDER BY created_at DESC`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var order Order
		var positionID sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Status, &positionID, &order.CreatedAt, &order.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}

		if positionID.Valid {
			id := int(positionID.Int64)
			order.PositionID = &id
		}

		orders = append(orders, order)
	}

	return orders, nil
}

// CleanupOldData supprime les anciens ordres terminés (plus anciens que X jours)
func (db *DB) CleanupOldData(olderThanDays int) error {
	cutoffDate := time.Now().AddDate(0, 0, -olderThanDays)

	// Supprimer les anciens ordres FILLED ou CANCELLED
	query := `DELETE FROM orders WHERE status IN (?, ?) AND updated_at < ?`
	result, err := db.conn.Exec(query, Filled, Cancelled, cutoffDate)
	if err != nil {
		return fmt.Errorf("failed to cleanup old orders: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		log.Printf("Cleaned up %d old orders", rowsAffected)
	}

	return nil
}

// GetPositionWithSellOrders récupère une position avec ses ordres de vente associés
func (db *DB) GetPositionWithSellOrders(positionID int) (*Position, []Order, error) {
	position, err := db.GetPosition(positionID)
	if err != nil {
		return nil, nil, err
	}

	query := `SELECT id, external_id, side, amount, price, status, position_id, created_at, updated_at 
		      FROM orders 
		      WHERE position_id = ? AND side = ?`
	rows, err := db.conn.Query(query, positionID, Sell)
	if err != nil {
		return position, nil, fmt.Errorf("failed to get sell orders for position: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var order Order
		var posID sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Status, &posID, &order.CreatedAt, &order.UpdatedAt)
		if err != nil {
			return position, nil, fmt.Errorf("failed to scan sell order: %w", err)
		}

		if posID.Valid {
			id := int(posID.Int64)
			order.PositionID = &id
		}

		orders = append(orders, order)
	}

	return position, orders, nil
}
