package database

import (
	"bot/internal/logger"
	"database/sql"
	"fmt"
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
	ID          int       `json:"id"`
	Price       float64   `json:"price"`
	Amount      float64   `json:"amount"`
	MaxPrice    float64   `json:"max_price"`
	TargetPrice float64   `json:"target_price"`
	StrategyID  *int      `json:"strategy_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Order struct {
	ID         int         `json:"id"`
	ExternalID string      `json:"external_id"` // ID de l'exchange
	Side       OrderSide   `json:"side"`
	Amount     float64     `json:"amount"`
	Price      float64     `json:"price"`
	Fees       float64     `json:"fees"`
	Status     OrderStatus `json:"status"`
	PositionID *int        `json:"position_id,omitempty"` // Pour lier les ordres de vente aux positions
	StrategyID *int        `json:"strategy_id,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type Cycle struct {
	ID         int       `json:"id"`
	BuyOrder   Order     `json:"buy_order"`
	SellOrder  *Order    `json:"sell_order,omitempty"`
	StrategyID *int      `json:"strategy_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Strategy struct {
	ID                   int        `json:"id"`
	Name                 string     `json:"name"`
	Description          string     `json:"description"`
	Enabled              bool       `json:"enabled"`
	AlgorithmName        string     `json:"algorithm_name"`
	CronExpression       string     `json:"cron_expression"`
	QuoteAmount          float64    `json:"quote_amount"`
	MaxConcurrentOrders  int        `json:"max_concurrent_orders"`
	RSIThreshold         *float64   `json:"rsi_threshold,omitempty"`
	RSIPeriod            *int       `json:"rsi_period,omitempty"`
	RSITimeframe         string     `json:"rsi_timeframe"`
	MACDFastPeriod       int        `json:"macd_fast_period"`
	MACDSlowPeriod       int        `json:"macd_slow_period"`
	MACDSignalPeriod     int        `json:"macd_signal_period"`
	MACDTimeframe        string     `json:"macd_timeframe"`
	BBPeriod             int        `json:"bb_period"`
	BBMultiplier         float64    `json:"bb_multiplier"`
	BBTimeframe          string     `json:"bb_timeframe"`
	ProfitTarget         float64    `json:"profit_target"`
	TrailingStopDelta    float64    `json:"trailing_stop_delta"`
	SellOffset           float64    `json:"sell_offset"`
	VolatilityPeriod     *int       `json:"volatility_period,omitempty"`
	VolatilityAdjustment *float64   `json:"volatility_adjustment,omitempty"`
	VolatilityTimeframe  string     `json:"volatility_timeframe"`
	LastExecutedAt       *time.Time `json:"last_executed_at,omitempty"`
	NextExecutionAt      *time.Time `json:"next_execution_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type Candle struct {
	ID         int       `json:"id"`
	Pair       string    `json:"pair"`
	Timeframe  string    `json:"timeframe"`
	Timestamp  int64     `json:"timestamp"`
	OpenPrice  float64   `json:"open_price"`
	HighPrice  float64   `json:"high_price"`
	LowPrice   float64   `json:"low_price"`
	ClosePrice float64   `json:"close_price"`
	Volume     float64   `json:"volume"`
	CreatedAt  time.Time `json:"created_at"`
}

type ActiveTimeframe struct {
	Pair      string `json:"pair"`
	Timeframe string `json:"timeframe"`
}

type Migration struct {
	ID   int
	Name string
	SQL  string
}

var migrations = []Migration{
	{
		ID:   1,
		Name: "init_schema",
		SQL: `
			CREATE TABLE IF NOT EXISTS positions (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				price REAL NOT NULL,
				amount REAL NOT NULL,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);
			CREATE TABLE IF NOT EXISTS orders (
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
			);
			CREATE INDEX IF NOT EXISTS idx_orders_external_id ON orders(external_id);
			CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
			CREATE INDEX IF NOT EXISTS idx_positions_created_at ON positions(created_at);
        `,
	},
	{
		ID:   2,
		Name: "add_max_price",
		SQL: `
            ALTER TABLE positions ADD COLUMN max_price REAL DEFAULT 0.0;
        `,
	},
	{
		ID:   3,
		Name: "add_order_fees",
		SQL: `
			ALTER TABLE orders ADD COLUMN fees REAL DEFAULT 0.0;
		`,
	},
	{
		ID:   4,
		Name: "create_cycles",
		SQL: `
			CREATE TABLE IF NOT EXISTS cycles (
			    id INTEGER PRIMARY KEY AUTOINCREMENT,
			    buy_order_id INTEGER NOT NULL,
			    sell_order_id INTEGER,
			    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(buy_order_id) REFERENCES orders(id),
				FOREIGN KEY(sell_order_id) REFERENCES orders(id) on delete set null
			);
		`,
	},
	{
		ID:   5,
		Name: "add_target_price_to_positions",
		SQL: `
			ALTER TABLE positions 
				ADD COLUMN target_price REAL DEFAULT 0.0;
		`,
	},
	{
		ID:   6,
		Name: "create_candles_table",
		SQL: `
			CREATE TABLE IF NOT EXISTS candles (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				pair TEXT NOT NULL,
				timeframe TEXT NOT NULL,
				timestamp INTEGER NOT NULL,
				open_price REAL NOT NULL,
				high_price REAL NOT NULL,
				low_price REAL NOT NULL,
				close_price REAL NOT NULL,
				volume REAL NOT NULL,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(pair, timeframe, timestamp)
			);
			CREATE INDEX IF NOT EXISTS idx_candles_pair_timeframe_timestamp ON candles(pair, timeframe, timestamp);
			CREATE INDEX IF NOT EXISTS idx_candles_timestamp ON candles(timestamp);
		`,
	},
	{
		ID:   7,
		Name: "create_strategies_table",
		SQL: `
			CREATE TABLE IF NOT EXISTS strategies (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT UNIQUE NOT NULL,
				description TEXT,
				enabled BOOLEAN DEFAULT 1,
				algorithm_name TEXT NOT NULL DEFAULT 'rsi_dca',
				cron_expression TEXT NOT NULL,
				quote_amount REAL NOT NULL,
				max_concurrent_orders INTEGER DEFAULT 1,
				
				-- RSI parameters
				rsi_threshold REAL,
				rsi_period INTEGER,
				rsi_timeframe TEXT DEFAULT '4h',
				
				-- MACD parameters (for future algorithms)
				macd_fast_period INTEGER DEFAULT 12,
				macd_slow_period INTEGER DEFAULT 26,
				macd_signal_period INTEGER DEFAULT 9,
				macd_timeframe TEXT DEFAULT '4h',
				
				-- Bollinger Bands parameters (for future algorithms)
				bb_period INTEGER DEFAULT 20,
				bb_multiplier REAL DEFAULT 2.0,
				bb_timeframe TEXT DEFAULT '1h',
				
				-- Common sell parameters
				profit_target REAL NOT NULL,
				trailing_stop_delta REAL NOT NULL,
				sell_offset REAL NOT NULL,
				
				-- Volatility parameters
				volatility_period INTEGER,
				volatility_adjustment REAL,
				volatility_timeframe TEXT DEFAULT '4h',
				
				-- Scheduling
				last_executed_at DATETIME NULL,
				next_execution_at DATETIME NULL,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_strategies_enabled ON strategies(enabled);
			CREATE INDEX IF NOT EXISTS idx_strategies_next_execution ON strategies(next_execution_at);
		`,
	},
	{
		ID:   8,
		Name: "add_strategy_id_columns",
		SQL: `
			ALTER TABLE orders ADD COLUMN strategy_id INTEGER NULL REFERENCES strategies(id) ON DELETE SET NULL;
			ALTER TABLE positions ADD COLUMN strategy_id INTEGER NULL REFERENCES strategies(id) ON DELETE SET NULL;
			ALTER TABLE cycles ADD COLUMN strategy_id INTEGER NULL REFERENCES strategies(id) ON DELETE SET NULL;
			
			CREATE INDEX IF NOT EXISTS idx_orders_strategy_id ON orders(strategy_id);
			CREATE INDEX IF NOT EXISTS idx_positions_strategy_id ON positions(strategy_id);
			CREATE INDEX IF NOT EXISTS idx_cycles_strategy_id ON cycles(strategy_id);
		`,
	},
	{
		ID:   9,
		Name: "create_legacy_strategy_and_migrate_data",
		SQL: `
			INSERT INTO strategies (id, name, description, algorithm_name, cron_expression,
				quote_amount, rsi_threshold, rsi_period, rsi_timeframe,
				volatility_period, volatility_adjustment, volatility_timeframe,
				profit_target, trailing_stop_delta, sell_offset, max_concurrent_orders)
			VALUES (1, 'Legacy Strategy', 'Migrated from single-strategy configuration', 'rsi_dca', '0 */4 * * *',
				50.0, 70.0, 14, '4h',
				7, 50.0, '4h',
				2.0, 0.1, 0.1, 1);
			
			UPDATE orders SET strategy_id = 1 WHERE strategy_id IS NULL;
			UPDATE positions SET strategy_id = 1 WHERE strategy_id IS NULL;
			UPDATE cycles SET strategy_id = 1 WHERE strategy_id IS NULL;
		`,
	},
}

type DB struct {
	conn *sql.DB
}

func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	_, err = conn.Exec("PRAGMA journal_mode=WAL;")
	if err != nil {
		return nil, fmt.Errorf("failed to activate WAL for database: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.applyMigrations(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to apply migrations: %v", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) applyMigrations() error {
	_, err := db.conn.Exec(`
        CREATE TABLE IF NOT EXISTS migrations (
            id INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        )
    `)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %v", err)
	}

	applied := make(map[int]bool)
	rows, err := db.conn.Query(`SELECT id FROM migrations`)
	if err != nil {
		return fmt.Errorf("failed to query migrations: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("failed to scan migration id: %v", err)
		}
		applied[id] = true
	}

	for _, migration := range migrations {
		if !applied[migration.ID] {
			logger.Infof("Applying migration %d: %s", migration.ID, migration.Name)
			_, err := db.conn.Exec(migration.SQL)
			if err != nil {
				return fmt.Errorf("failed to apply migration %d (%s): %v", migration.ID, migration.Name, err)
			}
			_, err = db.conn.Exec(`INSERT INTO migrations (id, name) VALUES (?, ?);`, migration.ID, migration.Name)
			if err != nil {
				return fmt.Errorf("failed to record migration %d (%s): %v", migration.ID, migration.Name, err)
			}
			logger.Infof("Migration %d (%s) applied successfully", migration.ID, migration.Name)
		}
	}

	return nil
}

// Positions
func (db *DB) CreatePosition(price, targetPrice, amount float64) (*Position, error) {
	query := `INSERT INTO positions (price, target_price, amount) VALUES (?, ?, ?)`
	result, err := db.conn.Exec(query, price, targetPrice, amount)
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
	query := `
		SELECT id, price, amount, max_price, target_price, strategy_id, created_at, updated_at
		FROM positions
		WHERE id = ?
	`
	row := db.conn.QueryRow(query, id)

	var pos Position
	var strategyID sql.NullInt64
	err := row.Scan(&pos.ID, &pos.Price, &pos.Amount, &pos.MaxPrice, &pos.TargetPrice, &strategyID, &pos.CreatedAt, &pos.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get position: %w", err)
	}

	if strategyID.Valid {
		id := int(strategyID.Int64)
		pos.StrategyID = &id
	}

	return &pos, nil
}

func (db *DB) GetAllPositions() ([]Position, error) {
	query := `
		SELECT id, price, amount, max_price, target_price, created_at, updated_at 
		FROM positions 
		ORDER BY created_at DESC
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}
	defer rows.Close()

	var positions []Position
	for rows.Next() {
		var pos Position
		err := rows.Scan(&pos.ID, &pos.Price, &pos.Amount, &pos.MaxPrice, &pos.TargetPrice, &pos.CreatedAt, &pos.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan position: %w", err)
		}
		positions = append(positions, pos)
	}

	return positions, nil
}

func (db *DB) GetOpenPositions() ([]Position, error) {
	query := `
		SELECT p.id, p.price, p.amount, p.max_price, p.target_price, p.created_at, p.updated_at 
		FROM positions p
		WHERE not exists (
			SELECT * FROM orders o 
			WHERE o.position_id = p.id and o.status = 'PENDING'
		)
		ORDER BY created_at DESC
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get open positions: %w", err)
	}
	defer rows.Close()

	var positions []Position
	for rows.Next() {
		var pos Position
		err := rows.Scan(&pos.ID, &pos.Price, &pos.Amount, &pos.MaxPrice, &pos.TargetPrice, &pos.CreatedAt, &pos.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan position: %w", err)
		}
		positions = append(positions, pos)
	}

	return positions, nil
}

func (db *DB) UpdatePositionMaxPrice(id int, maxPrice float64) error {
	query := `UPDATE positions SET max_price = ? WHERE id = ?`
	_, err := db.conn.Exec(query, maxPrice, id)
	if err != nil {
		return fmt.Errorf("failed to update max_price for position %d: %v", id, err)
	}
	return nil
}

func (db *DB) UpdatePositionTargetPrice(id int, targetPrice float64) error {
	query := `UPDATE positions SET target_price = ? WHERE id = ?`
	_, err := db.conn.Exec(query, targetPrice, id)
	if err != nil {
		return fmt.Errorf("failed to update target_price for position %d: %v", id, err)
	}
	return nil
}

func (db *DB) DeletePosition(id int) error {
	query := `DELETE FROM positions WHERE id = ?`
	_, err := db.conn.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete position: %d: %w", id, err)
	}
	return nil
}

// Orders
func (db *DB) CreateOrder(externalID string, side OrderSide, amount, price, fees float64, positionID *int) (*Order, error) {
	query := `INSERT INTO orders (external_id, side, amount, price, fees, status, position_id) VALUES (?, ?, ?, ?, ?, ?, ?)`
	result, err := db.conn.Exec(query, externalID, side, amount, price, fees, Pending, positionID)
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
	query := `
		SELECT id, external_id, side, amount, price, fees, status, position_id, strategy_id, created_at, updated_at
		FROM orders
		WHERE id = ?
	`
	row := db.conn.QueryRow(query, id)

	var order Order
	var positionID, strategyID sql.NullInt64
	err := row.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
		&order.Status, &positionID, &strategyID, &order.CreatedAt, &order.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	if positionID.Valid {
		id := int(positionID.Int64)
		order.PositionID = &id
	}
	if strategyID.Valid {
		id := int(strategyID.Int64)
		order.StrategyID = &id
	}

	return &order, nil
}

func (db *DB) GetOrderByExternalID(externalID string) (*Order, error) {
	query := `
		SELECT id, external_id, side, amount, price, fees, status, position_id, created_at, updated_at 
		FROM orders 
		WHERE external_id = ?
	`
	row := db.conn.QueryRow(query, externalID)

	var order Order
	var positionID sql.NullInt64
	err := row.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
		&order.Status, &positionID, &order.CreatedAt, &order.UpdatedAt)
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
	query := `
		SELECT id, external_id, side, amount, price, fees, status, position_id, created_at, updated_at 
		FROM orders 
		WHERE status = ? 
		ORDER BY created_at ASC
	`
	rows, err := db.conn.Query(query, Pending)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var order Order
		var positionID sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
			&order.Status, &positionID, &order.CreatedAt, &order.UpdatedAt)
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
	query := `
		UPDATE orders SET 
			status = ?, 
			updated_at = CURRENT_TIMESTAMP 
		WHERE external_id = ?
	`
	_, err := db.conn.Exec(query, status, externalID)
	if err != nil {
		return fmt.Errorf("failed to update order status: %w", err)
	}
	return nil
}

func (db *DB) UpdateOrderPosition(id int, positionId int) error {
	query := `
		UPDATE orders SET 
			position_id = ?, 
			updated_at = CURRENT_TIMESTAMP 
		WHERE id = ?
	`
	_, err := db.conn.Exec(query, positionId, id)
	if err != nil {
		return fmt.Errorf("failed to update order position: %w", err)
	}
	return nil
}

func (db *DB) GetOldOrders(olderThan time.Time) ([]Order, error) {
	query := `
		SELECT id, external_id, side, amount, price, fees, status, position_id, created_at, updated_at 
		FROM orders 
		WHERE status = ? AND created_at < ?
	`
	rows, err := db.conn.Query(query, Pending, olderThan)
	if err != nil {
		return nil, fmt.Errorf("failed to get old orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var order Order
		var positionID sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
			&order.Status, &positionID, &order.CreatedAt, &order.UpdatedAt)
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

// Cycles
func (db *DB) CreateCycle(buyOrderId int) (*Cycle, error) {
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

func (db *DB) GetCycle(id int) (*Cycle, error) {
	query := `
		SELECT id, buy_order_id, sell_order_id, strategy_id, created_at, updated_at
		FROM cycles
		WHERE id = ?
	`
	row := db.conn.QueryRow(query, id)

	var cycle Cycle
	var buyOrderId int64
	var sellOrderId, strategyID sql.NullInt64
	err := row.Scan(&cycle.ID, &buyOrderId, &sellOrderId, &strategyID, &cycle.CreatedAt, &cycle.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get cycle: %w", err)
	}

	if strategyID.Valid {
		id := int(strategyID.Int64)
		cycle.StrategyID = &id
	}

	buyOrder, err := db.GetOrder(int(buyOrderId))
	if err != nil {
		return nil, fmt.Errorf("failed to get cycle: %w", err)
	}
	cycle.BuyOrder = *buyOrder

	if sellOrderId.Valid {
		id := int(sellOrderId.Int64)
		sellOrder, err := db.GetOrder(id)
		if err != nil {
			return nil, fmt.Errorf("failed to get cycle: %w", err)
		}
		cycle.SellOrder = sellOrder
	}

	return &cycle, nil
}

func (db *DB) GetCycleForBuyOrder(buyOrderId int) (*Cycle, error) {
	var cycleId sql.NullInt64
	err := db.conn.QueryRow(`SELECT id from cycles WHERE buy_order_id = ?`, buyOrderId).Scan(&cycleId)
	if err != nil {
		return nil, fmt.Errorf("failed to get cycle id for buy order: %w", err)
	}
	if cycleId.Valid {
		return db.GetCycle(int(cycleId.Int64))
	} else {
		return nil, fmt.Errorf("failed to get cycle id for buy order: %w", err)
	}
}

func (db *DB) GetCycleForBuyOrderPosition(buyOrderPositionId int) (*Cycle, error) {
	var cycleId sql.NullInt64
	query := `
		SELECT c.id 
		FROM cycles c 
		JOIN orders bo ON c.buy_order_id = bo.id 
		WHERE bo.position_id= ?
	`
	err := db.conn.QueryRow(query, buyOrderPositionId).Scan(&cycleId)
	if err != nil {
		return nil, fmt.Errorf("failed to get cycle id for buy order by position: %w", err)
	}
	if cycleId.Valid {
		return db.GetCycle(int(cycleId.Int64))
	} else {
		return nil, fmt.Errorf("failed to get cycle id for buy order by position: %w", err)
	}
}

func (db *DB) GetCycleForSellOrder(sellOrderId int) (*Cycle, error) {
	var cycleId sql.NullInt64
	err := db.conn.QueryRow(`SELECT id from cycles WHERE sell_order_id = ?`, sellOrderId).Scan(&cycleId)
	if err != nil {
		return nil, fmt.Errorf("failed to get cycle id for buy order: %w", err)
	}
	if cycleId.Valid {
		return db.GetCycle(int(cycleId.Int64))
	} else {
		return nil, fmt.Errorf("failed to get cycle id for buy order: %w", err)
	}
}

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

func (db *DB) GetAllCycles() ([]Cycle, error) {
	query := `
		SELECT id, buy_order_id, sell_order_id, created_at, updated_at
		FROM cycles 
		ORDER BY created_at DESC
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get cycles: %w", err)
	}
	defer rows.Close()

	var cycles []Cycle
	for rows.Next() {
		var cycle Cycle
		var buyOrderId int64
		var sellOrderId sql.NullInt64
		err := rows.Scan(&cycle.ID, &buyOrderId, &sellOrderId, &cycle.CreatedAt, &cycle.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan position: %w", err)
		}

		buyOrder, err := db.GetOrder(int(buyOrderId))
		if err != nil {
			return nil, fmt.Errorf("failed to get cycle: %w", err)
		}
		cycle.BuyOrder = *buyOrder

		if sellOrderId.Valid {
			id := int(sellOrderId.Int64)
			sellOrder, err := db.GetOrder(id)
			if err != nil {
				return nil, fmt.Errorf("failed to get cycle: %w", err)
			}
			cycle.SellOrder = sellOrder
		}
		cycles = append(cycles, cycle)
	}

	return cycles, nil
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

	// Nombre de cycles
	var cyclesCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM cycles`).Scan(&cyclesCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get cycles count: %w", err)
	}
	stats["cycles_count"] = cyclesCount

	// Nombre de cycles en cours
	var activeCyclesCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM cycles where sell_order_id is NULL`).Scan(&activeCyclesCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get active cycles count: %w", err)
	}
	stats["active_cycles_count"] = activeCyclesCount

	// Nombre de cycles complets
	var completedCyclesCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM cycles where sell_order_id is NOT NULL`).Scan(&completedCyclesCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get completed cycles count: %w", err)
	}
	stats["completed_cycles_count"] = completedCyclesCount

	// Calcul du profit moyen
	var avgProfit sql.NullFloat64
	err = db.conn.QueryRow(`
		SELECT AVG((so.price - bo.price) * bo.amount - bo.fees - so.fees)
		FROM cycles c 
		JOIN orders bo ON c.buy_order_id = bo.id 
		JOIN orders so ON c.sell_order_id = so.id 
		WHERE so.id IS NOT NULL
	`).Scan(&avgProfit)
	if err != nil {
		return nil, fmt.Errorf("failed to get average profit: %w", err)
	}
	if avgProfit.Valid {
		stats["average_profit"] = avgProfit.Float64
	} else {
		stats["average_profit"] = 0.0
	}

	// Calcul du profit total
	var totalProfit sql.NullFloat64
	err = db.conn.QueryRow(`
		SELECT SUM((so.price - bo.price) * bo.amount - bo.fees - so.fees)
		FROM cycles c 
		JOIN orders bo ON c.buy_order_id = bo.id 
		JOIN orders so ON c.sell_order_id = so.id 
		WHERE so.id IS NOT NULL
	`).Scan(&totalProfit)
	if err != nil {
		return nil, fmt.Errorf("failed to get total profit: %w", err)
	}
	if totalProfit.Valid {
		stats["total_profit"] = totalProfit.Float64
	} else {
		stats["total_profit"] = 0.0
	}

	return stats, nil
}

// GetAllOrders récupère tous les ordres (pas seulement pending)
func (db *DB) GetAllOrders() ([]Order, error) {
	query := `
		SELECT id, external_id, side, amount, price, fees, status, position_id, created_at, updated_at 
		FROM orders 
		ORDER BY created_at DESC
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var order Order
		var positionID sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
			&order.Status, &positionID, &order.CreatedAt, &order.UpdatedAt)
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
		logger.Infof("Cleaned up %d old orders", rowsAffected)
	}

	return nil
}

// GetPositionWithSellOrders récupère une position avec ses ordres de vente associés
func (db *DB) GetPositionWithSellOrders(positionID int) (*Position, []Order, error) {
	position, err := db.GetPosition(positionID)
	if err != nil {
		return nil, nil, err
	}

	query := `
		SELECT id, external_id, side, amount, price, fees, status, position_id, created_at, updated_at 
		FROM orders 
		WHERE position_id = ? AND side = ?
	`
	rows, err := db.conn.Query(query, positionID, Sell)
	if err != nil {
		return position, nil, fmt.Errorf("failed to get sell orders for position: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var order Order
		var posID sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
			&order.Status, &posID, &order.CreatedAt, &order.UpdatedAt)
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

// GetProfitStats calcule les statistiques de profit des cycles terminés
func (db *DB) GetProfitStats() (avgProfit, totalProfit float64, err error) {
	query := `
		SELECT 
			(so.price - bo.price) * bo.amount - bo.fees - so.fees as profit
		FROM cycles c 
		JOIN orders bo ON c.buy_order_id = bo.id 
		JOIN orders so ON c.sell_order_id = so.id 
		WHERE so.id IS NOT NULL AND bo.status = 'FILLED' AND so.status = 'FILLED'
	`

	rows, err := db.conn.Query(query)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get profit stats: %w", err)
	}
	defer rows.Close()

	profits := []float64{}
	for rows.Next() {
		var profit float64
		if err := rows.Scan(&profit); err != nil {
			continue
		}
		profits = append(profits, profit)
		totalProfit += profit
	}

	if len(profits) > 0 {
		avgProfit = totalProfit / float64(len(profits))
	}

	return avgProfit, totalProfit, nil
}

// GetCycleWithProfit récupère un cycle avec son profit calculé
type CycleWithProfit struct {
	Cycle
	Profit   *float64 `json:"profit,omitempty"`
	Duration *string  `json:"duration,omitempty"`
}

func (db *DB) GetAllCyclesWithProfit() ([]CycleWithProfit, error) {
	cycles, err := db.GetAllCycles()
	if err != nil {
		return nil, err
	}

	cyclesWithProfit := make([]CycleWithProfit, len(cycles))

	for i, cycle := range cycles {
		cycleWithProfit := CycleWithProfit{Cycle: cycle}

		// Calculer le profit si le cycle est terminé
		if cycle.SellOrder != nil {
			profit := (cycle.SellOrder.Price - cycle.BuyOrder.Price) * cycle.BuyOrder.Amount
			profit -= cycle.BuyOrder.Fees
			profit -= cycle.SellOrder.Fees
			cycleWithProfit.Profit = &profit

			// Calculer la durée
			duration := cycle.SellOrder.CreatedAt.Sub(cycle.CreatedAt)
			durationStr := formatDuration(duration)
			cycleWithProfit.Duration = &durationStr
		}

		cyclesWithProfit[i] = cycleWithProfit
	}

	return cyclesWithProfit, nil
}

// Helper pour formater la durée
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d sec", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%d min", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
	} else {
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		return fmt.Sprintf("%d j %d h", days, hours)
	}
}

// GetOrdersWithPagination récupère les ordres avec pagination
func (db *DB) GetOrdersWithPagination(status OrderStatus, limit, offset int) ([]Order, int, error) {
	// Compter le total
	var total int
	countQuery := `SELECT COUNT(*) FROM orders`
	var args []interface{}

	if status != "" {
		countQuery += ` WHERE status = ?`
		args = append(args, status)
	}

	err := db.conn.QueryRow(countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count orders: %w", err)
	}

	// Récupérer les ordres
	query := `
		SELECT id, external_id, side, amount, price, fees, status, position_id, created_at, updated_at 
		FROM orders
	`

	if status != "" {
		query += ` WHERE status = ?`
	}

	query += ` ORDER BY created_at DESC LIMIT ? OFFSET ?`

	if status != "" {
		args = append(args, limit, offset)
	} else {
		args = []interface{}{limit, offset}
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get orders with pagination: %w", err)
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var order Order
		var positionID sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
			&order.Status, &positionID, &order.CreatedAt, &order.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan order: %w", err)
		}

		if positionID.Valid {
			id := int(positionID.Int64)
			order.PositionID = &id
		}

		orders = append(orders, order)
	}

	return orders, total, nil
}

// GetRecentActivity récupère l'activité récente (pour le dashboard)
func (db *DB) GetRecentActivity(limit int) ([]map[string]interface{}, error) {
	query := `
		SELECT 
			'order' as type,
			id,
			external_id as reference,
			side,
			price,
			amount,
			status,
			created_at,
			updated_at
		FROM orders 
		WHERE updated_at > datetime('now', '-24 hours')
		UNION ALL
		SELECT 
			'cycle' as type,
			id,
			'#' || id as reference,
			CASE WHEN sell_order_id IS NULL THEN 'ACTIVE' ELSE 'COMPLETED' END as side,
			0 as price,
			0 as amount,
			CASE WHEN sell_order_id IS NULL THEN 'ACTIVE' ELSE 'COMPLETED' END as status,
			created_at,
			updated_at
		FROM cycles
		WHERE updated_at > datetime('now', '-24 hours')
		ORDER BY updated_at DESC
		LIMIT ?
	`

	rows, err := db.conn.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent activity: %w", err)
	}
	defer rows.Close()

	var activities []map[string]interface{}
	for rows.Next() {
		var activity = make(map[string]interface{})
		var actType, reference, side, status string
		var id int
		var price, amount float64
		var createdAt, updatedAt time.Time

		err := rows.Scan(&actType, &id, &reference, &side, &price, &amount, &status, &createdAt, &updatedAt)
		if err != nil {
			continue
		}

		activity["type"] = actType
		activity["id"] = id
		activity["reference"] = reference
		activity["side"] = side
		activity["price"] = price
		activity["amount"] = amount
		activity["status"] = status
		activity["created_at"] = createdAt
		activity["updated_at"] = updatedAt

		activities = append(activities, activity)
	}

	return activities, nil
}

// GetDashboardMetrics récupère toutes les métriques pour le dashboard
func (db *DB) GetDashboardMetrics() (map[string]interface{}, error) {
	stats, err := db.GetStats()
	if err != nil {
		return nil, err
	}

	// Ajouter les profits
	avgProfit, totalProfit, err := db.GetProfitStats()
	if err != nil {
		return nil, err
	}

	stats["avg_profit"] = avgProfit
	stats["total_profit"] = totalProfit

	// Calculer le taux de réussite
	filled, _ := stats["filled_orders"].(int)
	cancelled, _ := stats["cancelled_orders"].(int)
	pending, _ := stats["pending_orders"].(int)
	totalOrders := filled + cancelled + pending

	if totalOrders > 0 {
		stats["success_rate"] = (float64(filled) / float64(totalOrders)) * 100
	} else {
		stats["success_rate"] = 0.0
	}

	// Activité récente
	recentActivity, err := db.GetRecentActivity(10)
	if err == nil {
		stats["recent_activity"] = recentActivity
	}

	return stats, nil
}

func (db *DB) CalculateProfitStats() (avgProfit float64, totalProfit float64) {
	// Requête pour calculer les profits des cycles terminés
	query := `
		SELECT 
			(so.price - bo.price) * bo.amount - bo.fees - coalesce(so.fees, 0) as profit
		FROM cycles c 
		JOIN orders bo ON c.buy_order_id = bo.id 
		LEFT JOIN orders so ON c.sell_order_id = so.id 
		WHERE so.id IS NOT NULL AND bo.status = 'FILLED' AND so.status = 'FILLED'
	`

	rows, err := db.conn.Query(query)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()

	profits := []float64{}
	for rows.Next() {
		var profit float64
		if err := rows.Scan(&profit); err != nil {
			continue
		}
		profits = append(profits, profit)
		totalProfit += profit
	}

	if len(profits) > 0 {
		avgProfit = totalProfit / float64(len(profits))
	}

	return avgProfit, totalProfit
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

// ===============================
// STRATEGIES METHODS
// ===============================

func (db *DB) CreateStrategy(name, description, algorithmName, cronExpression string, quoteAmount, profitTarget, trailingStopDelta, sellOffset float64) (*Strategy, error) {
	query := `INSERT INTO strategies (name, description, algorithm_name, cron_expression, quote_amount, profit_target, trailing_stop_delta, sell_offset) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	result, err := db.conn.Exec(query, name, description, algorithmName, cronExpression, quoteAmount, profitTarget, trailingStopDelta, sellOffset)
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return db.GetStrategy(int(id))
}

func (db *DB) GetStrategy(id int) (*Strategy, error) {
	query := `
		SELECT id, name, description, enabled, algorithm_name, cron_expression, quote_amount, max_concurrent_orders,
			rsi_threshold, rsi_period, rsi_timeframe, macd_fast_period, macd_slow_period, macd_signal_period, macd_timeframe,
			bb_period, bb_multiplier, bb_timeframe, profit_target, trailing_stop_delta, sell_offset,
			volatility_period, volatility_adjustment, volatility_timeframe,
			last_executed_at, next_execution_at, created_at, updated_at
		FROM strategies
		WHERE id = ?
	`
	row := db.conn.QueryRow(query, id)

	var strategy Strategy
	var lastExecutedAt, nextExecutionAt sql.NullTime
	var rsiThreshold sql.NullFloat64
	var rsiPeriod sql.NullInt64
	var volatilityPeriod sql.NullInt64
	var volatilityAdjustment sql.NullFloat64

	err := row.Scan(&strategy.ID, &strategy.Name, &strategy.Description, &strategy.Enabled,
		&strategy.AlgorithmName, &strategy.CronExpression, &strategy.QuoteAmount, &strategy.MaxConcurrentOrders,
		&rsiThreshold, &rsiPeriod, &strategy.RSITimeframe, &strategy.MACDFastPeriod, &strategy.MACDSlowPeriod,
		&strategy.MACDSignalPeriod, &strategy.MACDTimeframe, &strategy.BBPeriod, &strategy.BBMultiplier, &strategy.BBTimeframe,
		&strategy.ProfitTarget, &strategy.TrailingStopDelta, &strategy.SellOffset,
		&volatilityPeriod, &volatilityAdjustment, &strategy.VolatilityTimeframe,
		&lastExecutedAt, &nextExecutionAt, &strategy.CreatedAt, &strategy.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to get strategy: %w", err)
	}

	// Handle nullable fields
	if rsiThreshold.Valid {
		strategy.RSIThreshold = &rsiThreshold.Float64
	}
	if rsiPeriod.Valid {
		period := int(rsiPeriod.Int64)
		strategy.RSIPeriod = &period
	}
	if volatilityPeriod.Valid {
		period := int(volatilityPeriod.Int64)
		strategy.VolatilityPeriod = &period
	}
	if volatilityAdjustment.Valid {
		strategy.VolatilityAdjustment = &volatilityAdjustment.Float64
	}
	if lastExecutedAt.Valid {
		strategy.LastExecutedAt = &lastExecutedAt.Time
	}
	if nextExecutionAt.Valid {
		strategy.NextExecutionAt = &nextExecutionAt.Time
	}

	return &strategy, nil
}

func (db *DB) GetEnabledStrategies() ([]Strategy, error) {
	query := `
		SELECT id, name, description, enabled, algorithm_name, cron_expression, quote_amount, max_concurrent_orders,
			rsi_threshold, rsi_period, rsi_timeframe, macd_fast_period, macd_slow_period, macd_signal_period, macd_timeframe,
			bb_period, bb_multiplier, bb_timeframe, profit_target, trailing_stop_delta, sell_offset,
			volatility_period, volatility_adjustment, volatility_timeframe,
			last_executed_at, next_execution_at, created_at, updated_at
		FROM strategies
		WHERE enabled = 1
		ORDER BY created_at DESC
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get enabled strategies: %w", err)
	}
	defer rows.Close()

	var strategies []Strategy
	for rows.Next() {
		var strategy Strategy
		var lastExecutedAt, nextExecutionAt sql.NullTime
		var rsiThreshold sql.NullFloat64
		var rsiPeriod sql.NullInt64
		var volatilityPeriod sql.NullInt64
		var volatilityAdjustment sql.NullFloat64

		err := rows.Scan(&strategy.ID, &strategy.Name, &strategy.Description, &strategy.Enabled,
			&strategy.AlgorithmName, &strategy.CronExpression, &strategy.QuoteAmount, &strategy.MaxConcurrentOrders,
			&rsiThreshold, &rsiPeriod, &strategy.RSITimeframe, &strategy.MACDFastPeriod, &strategy.MACDSlowPeriod,
			&strategy.MACDSignalPeriod, &strategy.MACDTimeframe, &strategy.BBPeriod, &strategy.BBMultiplier, &strategy.BBTimeframe,
			&strategy.ProfitTarget, &strategy.TrailingStopDelta, &strategy.SellOffset,
			&volatilityPeriod, &volatilityAdjustment, &strategy.VolatilityTimeframe,
			&lastExecutedAt, &nextExecutionAt, &strategy.CreatedAt, &strategy.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan strategy: %w", err)
		}

		// Handle nullable fields
		if rsiThreshold.Valid {
			strategy.RSIThreshold = &rsiThreshold.Float64
		}
		if rsiPeriod.Valid {
			period := int(rsiPeriod.Int64)
			strategy.RSIPeriod = &period
		}
		if volatilityPeriod.Valid {
			period := int(volatilityPeriod.Int64)
			strategy.VolatilityPeriod = &period
		}
		if volatilityAdjustment.Valid {
			strategy.VolatilityAdjustment = &volatilityAdjustment.Float64
		}
		if lastExecutedAt.Valid {
			strategy.LastExecutedAt = &lastExecutedAt.Time
		}
		if nextExecutionAt.Valid {
			strategy.NextExecutionAt = &nextExecutionAt.Time
		}

		strategies = append(strategies, strategy)
	}

	return strategies, nil
}

func (db *DB) GetAllStrategies() ([]Strategy, error) {
	query := `
		SELECT id, name, description, enabled, algorithm_name, cron_expression, quote_amount, max_concurrent_orders,
			rsi_threshold, rsi_period, rsi_timeframe, macd_fast_period, macd_slow_period, macd_signal_period, macd_timeframe,
			bb_period, bb_multiplier, bb_timeframe, profit_target, trailing_stop_delta, sell_offset,
			volatility_period, volatility_adjustment, volatility_timeframe,
			last_executed_at, next_execution_at, created_at, updated_at
		FROM strategies
		ORDER BY created_at DESC
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get all strategies: %w", err)
	}
	defer rows.Close()

	var strategies []Strategy
	for rows.Next() {
		var strategy Strategy
		var lastExecutedAt, nextExecutionAt sql.NullTime
		var rsiThreshold sql.NullFloat64
		var rsiPeriod sql.NullInt64
		var volatilityPeriod sql.NullInt64
		var volatilityAdjustment sql.NullFloat64

		err := rows.Scan(&strategy.ID, &strategy.Name, &strategy.Description, &strategy.Enabled,
			&strategy.AlgorithmName, &strategy.CronExpression, &strategy.QuoteAmount, &strategy.MaxConcurrentOrders,
			&rsiThreshold, &rsiPeriod, &strategy.RSITimeframe, &strategy.MACDFastPeriod, &strategy.MACDSlowPeriod,
			&strategy.MACDSignalPeriod, &strategy.MACDTimeframe, &strategy.BBPeriod, &strategy.BBMultiplier, &strategy.BBTimeframe,
			&strategy.ProfitTarget, &strategy.TrailingStopDelta, &strategy.SellOffset,
			&volatilityPeriod, &volatilityAdjustment, &strategy.VolatilityTimeframe,
			&lastExecutedAt, &nextExecutionAt, &strategy.CreatedAt, &strategy.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan strategy: %w", err)
		}

		// Handle nullable fields
		if rsiThreshold.Valid {
			strategy.RSIThreshold = &rsiThreshold.Float64
		}
		if rsiPeriod.Valid {
			period := int(rsiPeriod.Int64)
			strategy.RSIPeriod = &period
		}
		if volatilityPeriod.Valid {
			period := int(volatilityPeriod.Int64)
			strategy.VolatilityPeriod = &period
		}
		if volatilityAdjustment.Valid {
			strategy.VolatilityAdjustment = &volatilityAdjustment.Float64
		}
		if lastExecutedAt.Valid {
			strategy.LastExecutedAt = &lastExecutedAt.Time
		}
		if nextExecutionAt.Valid {
			strategy.NextExecutionAt = &nextExecutionAt.Time
		}

		strategies = append(strategies, strategy)
	}

	return strategies, nil
}

func (db *DB) UpdateStrategyExecution(id int, lastExecuted time.Time) error {
	query := `UPDATE strategies SET last_executed_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := db.conn.Exec(query, lastExecuted, id)
	if err != nil {
		return fmt.Errorf("failed to update strategy execution time: %w", err)
	}
	return nil
}

// ===============================
// CANDLES METHODS
// ===============================

func (db *DB) SaveCandle(pair, timeframe string, timestamp int64, open, high, low, close, volume float64) error {
	query := `INSERT OR IGNORE INTO candles (pair, timeframe, timestamp, open_price, high_price, low_price, close_price, volume) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.conn.Exec(query, pair, timeframe, timestamp, open, high, low, close, volume)
	if err != nil {
		return fmt.Errorf("failed to save candle: %w", err)
	}
	return nil
}

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

func (db *DB) GetActiveTimeframes() ([]ActiveTimeframe, error) {
	query := `
		SELECT DISTINCT 'UBTC/USDC' as pair, s.rsi_timeframe as timeframe
		FROM strategies s
		WHERE s.enabled = 1 AND s.rsi_timeframe IS NOT NULL
		UNION
		SELECT DISTINCT 'UBTC/USDC' as pair, s.volatility_timeframe as timeframe
		FROM strategies s
		WHERE s.enabled = 1 AND s.volatility_timeframe IS NOT NULL
		UNION
		SELECT DISTINCT 'UBTC/USDC' as pair, s.macd_timeframe as timeframe
		FROM strategies s
		WHERE s.enabled = 1 AND s.macd_timeframe IS NOT NULL
		UNION
		SELECT DISTINCT 'UBTC/USDC' as pair, s.bb_timeframe as timeframe
		FROM strategies s
		WHERE s.enabled = 1 AND s.bb_timeframe IS NOT NULL
	`
	rows, err := db.conn.Query(query)
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

func (db *DB) CleanupOldCandles(olderThanDays int) error {
	cutoffTimestamp := time.Now().AddDate(0, 0, -olderThanDays).Unix()
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

// ===============================
// EXTENDED METHODS WITH STRATEGY SUPPORT
// ===============================

func (db *DB) CreateOrderWithStrategy(externalID string, side OrderSide, amount, price, fees float64, positionID *int, strategyID int) (*Order, error) {
	query := `INSERT INTO orders (external_id, side, amount, price, fees, status, position_id, strategy_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	result, err := db.conn.Exec(query, externalID, side, amount, price, fees, Pending, positionID, strategyID)
	if err != nil {
		return nil, fmt.Errorf("failed to create order with strategy: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return db.GetOrder(int(id))
}

func (db *DB) CreatePositionWithStrategy(price, targetPrice, amount float64, strategyID int) (*Position, error) {
	query := `INSERT INTO positions (price, target_price, amount, strategy_id) VALUES (?, ?, ?, ?)`
	result, err := db.conn.Exec(query, price, targetPrice, amount, strategyID)
	if err != nil {
		return nil, fmt.Errorf("failed to create position with strategy: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return db.GetPosition(int(id))
}

func (db *DB) CreateCycleWithStrategy(buyOrderId int, strategyID int) (*Cycle, error) {
	query := `INSERT INTO cycles (buy_order_id, strategy_id) VALUES (?, ?)`
	result, err := db.conn.Exec(query, buyOrderId, strategyID)
	if err != nil {
		return nil, fmt.Errorf("failed to create cycle with strategy: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return db.GetCycle(int(id))
}

// ===============================
// STRATEGY-SPECIFIC METHODS
// ===============================

func (db *DB) GetOpenPositionsForStrategy(strategyID int) ([]Position, error) {
	query := `
		SELECT p.id, p.price, p.amount, p.max_price, p.target_price, p.strategy_id, p.created_at, p.updated_at
		FROM positions p
		WHERE p.strategy_id = ? AND not exists (
			SELECT * FROM orders o
			WHERE o.position_id = p.id and o.status = 'PENDING' and o.side = 'SELL'
		)
		ORDER BY created_at DESC
	`
	rows, err := db.conn.Query(query, strategyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get open positions for strategy: %w", err)
	}
	defer rows.Close()

	var positions []Position
	for rows.Next() {
		var pos Position
		var strategyID sql.NullInt64
		err := rows.Scan(&pos.ID, &pos.Price, &pos.Amount, &pos.MaxPrice, &pos.TargetPrice, &strategyID, &pos.CreatedAt, &pos.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan position: %w", err)
		}

		if strategyID.Valid {
			id := int(strategyID.Int64)
			pos.StrategyID = &id
		}

		positions = append(positions, pos)
	}

	return positions, nil
}

func (db *DB) CountActiveOrdersForStrategy(strategyID int) (int, error) {
	query := `SELECT COUNT(*) FROM orders WHERE strategy_id = ? AND status = ?`
	var count int
	err := db.conn.QueryRow(query, strategyID, Pending).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count active orders for strategy: %w", err)
	}
	return count, nil
}

func (db *DB) UpdateStrategyNextExecution(strategyID int, nextExecution time.Time) error {
	query := `UPDATE strategies SET next_execution_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := db.conn.Exec(query, nextExecution, strategyID)
	if err != nil {
		return fmt.Errorf("failed to update strategy next execution: %w", err)
	}
	return nil
}

func (db *DB) GetStrategyStats(strategyID int) (map[string]interface{}, error) {
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
	err := db.conn.QueryRow(query, strategyID).Scan(&pending, &filled, &cancelled)
	if err != nil {
		return nil, fmt.Errorf("failed to get strategy order stats: %w", err)
	}

	stats["pending_orders"] = pending
	stats["filled_orders"] = filled
	stats["cancelled_orders"] = cancelled

	// Count positions for this strategy
	var positionsCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM positions WHERE strategy_id = ?`, strategyID).Scan(&positionsCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get strategy positions count: %w", err)
	}
	stats["active_positions"] = positionsCount

	return stats, nil
}

// ===============================
// PUBLIC METHODS FOR EXTERNAL TOOLS
// ===============================

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

	// Insert strategy
	query := `
		INSERT INTO strategies (
			name, description, enabled, algorithm_name, cron_expression, quote_amount,
			rsi_threshold, rsi_period, rsi_timeframe, profit_target, trailing_stop_delta, sell_offset,
			volatility_period, volatility_adjustment, volatility_timeframe, max_concurrent_orders
		) VALUES (?, ?, 1, ?, ?, ?, ?, ?, '4h', ?, 0.1, 0.1, 7, 50.0, '4h', 1)
	`

	_, err = db.conn.Exec(query, name, description, algorithm, cron, amount,
		rsiThresh, rsiPeriod, profitTarget)
	if err != nil {
		return fmt.Errorf("failed to create strategy: %w", err)
	}

	return nil
}

func (db *DB) DeleteNonLegacyStrategies() (int64, error) {
	result, err := db.conn.Exec(`DELETE FROM strategies WHERE id != 1`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete strategies: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}
