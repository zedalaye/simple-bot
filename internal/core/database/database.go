package database

import (
	"bot/internal/logger"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
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

// type Position struct {
// 	ID          int       `json:"id"`
// 	Price       float64   `json:"price"`
// 	Amount      float64   `json:"amount"`
// 	MaxPrice    float64   `json:"max_price"`
// 	TargetPrice float64   `json:"target_price"`
// 	StrategyID  *int      `json:"strategy_id,omitempty"`
// 	CreatedAt   time.Time `json:"created_at"`
// 	UpdatedAt   time.Time `json:"updated_at"`
// }

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

type Order struct {
	ID         int         `json:"id"`
	StrategyID *int        `json:"strategy_id,omitempty"`
	ExternalID string      `json:"external_id"` // ID de l'exchange
	Side       OrderSide   `json:"side"`
	Amount     float64     `json:"amount"`
	Price      float64     `json:"price"`
	Fees       float64     `json:"fees"`
	Status     OrderStatus `json:"status"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type Cycle struct {
	ID          int       `json:"id"`
	BuyOrder    Order     `json:"buy_order"`
	SellOrder   *Order    `json:"sell_order,omitempty"`
	MaxPrice    float64   `json:"max_price"`
	TargetPrice float64   `json:"target_price"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Le status du cycle est déterminé par le status des ordres d'achat et de vente
type CycleEnhanced struct {
	Cycle
	StrategyID int      `json:"strategy_id"`
	Status     string   `json:"status"`
	Profit     *float64 `json:"profit,omitempty"`
	Duration   string   `json:"duration"`
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
	{
		ID:   10,
		Name: "migrate_max_price_and_target_price_to_cycles",
		SQL: `
			ALTER TABLE cycles 
				ADD COLUMN max_price REAL NOT NULL DEFAULT 0.0;
			ALTER TABLE cycles 
				ADD COLUMN target_price REAL NOT NULL DEFAULT 0.0;

			UPDATE cycles SET 
			  max_price = (
					SELECT p.max_price 
					FROM positions p 
					JOIN orders bo ON p.id = bo.position_id 
					WHERE bo.id = cycles.buy_order_id
				);

			UPDATE cycles SET
				target_price = (
					SELECT p.target_price 
					FROM positions p 
					JOIN orders bo ON p.id = bo.position_id 
					WHERE bo.id = cycles.buy_order_id
				);
		`,
	},
	{
		ID:   11,
		Name: "remove_position_id_from_orders",
		SQL: `
			CREATE TABLE IF NOT EXISTS orders2 (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				strategy_id INTEGER NULL,
				external_id TEXT UNIQUE NOT NULL,
				side TEXT NOT NULL CHECK(side IN ('BUY', 'SELL')),
				amount REAL NOT NULL,
				price REAL NOT NULL,
				fees REAL NOT NULL DEFAULT 0.0,
				status TEXT NOT NULL CHECK(status IN ('PENDING', 'FILLED', 'CANCELLED')),
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY(strategy_id) REFERENCES strategies(id) ON DELETE SET NULL
			);
		
			INSERT INTO orders2 (id, strategy_id, external_id, side, amount, price, fees, status, created_at, updated_at)
				SELECT id, strategy_id, external_id, side, amount, price, fees, status, created_at, updated_at 
				FROM orders;

			DROP TABLE orders;
			ALTER TABLE orders2 RENAME TO orders;

			CREATE INDEX IF NOT EXISTS idx_orders_external_id ON orders(external_id);
			CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
			CREATE INDEX IF NOT EXISTS idx_orders_strategy_id ON orders(strategy_id);
		`,
	},
	{
		ID:   12,
		Name: "remove_strategy_id_from_cycles",
		SQL: `
			CREATE TABLE cycles2 (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				buy_order_id INTEGER NOT NULL,
				sell_order_id INTEGER,
				target_price REAL NOT NULL DEFAULT 0.0,
				max_price REAL NOT NULL DEFAULT 0.0, 
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP, 
				FOREIGN KEY(buy_order_id) REFERENCES orders(id),
				FOREIGN KEY(sell_order_id) REFERENCES orders(id) ON DELETE SET NULL
			);

			INSERT INTO cycles2 (id, buy_order_id, sell_order_id, target_price, max_price, created_at, updated_at)
				SELECT id, buy_order_id, sell_order_id, target_price, max_price, created_at, updated_at 
				FROM cycles;

			DROP TABLE cycles;
			ALTER TABLE cycles2 RENAME TO cycles;

			CREATE INDEX IF NOT EXISTS idx_cycles_buy_order_id ON cycles(buy_order_id);
			CREATE INDEX IF NOT EXISTS idx_cycles_sell_order_id ON cycles(sell_order_id);
		`,
	},
	{
		ID:   13,
		Name: "remove_positions",
		SQL: `
			DROP TABLE positions;
		`,
	},
}

type DB struct {
	conn *sql.DB
}

func NewDB(dbPath string) (*DB, error) {
	dbPath, err := filepath.Abs(dbPath)
	if err != nil {
		log.Fatalf("Failed to get absolute path for database: %v", err)
	}

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

func (db *DB) GetOpenCycles() ([]CycleEnhanced, error) {
	query := `
		SELECT c.id
		FROM cycles c
		LEFT OUTER JOIN orders bo on (bo.id = c.buy_order_id)
		LEFT OUTER JOIN orders so on (so.id = c.sell_order_id)
		WHERE (bo.status = 'FILLED') 
		  and (c.sell_order_id is null or (so.status = 'CANCELLED'))
		ORDER BY c.created_at DESC
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get open cycles: %w", err)
	}
	defer rows.Close()

	var cycles []CycleEnhanced
	for rows.Next() {
		var id int
		err := rows.Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cycle: %w", err)
		}
		cycle, err := db.GetCycle(id)
		if err != nil {
			return nil, fmt.Errorf("failed to get cycle: %w", err)
		}
		cycles = append(cycles, *cycle)
	}

	return cycles, nil
}

func (db *DB) UpdateCycleMaxPrice(id int, maxPrice float64) error {
	query := `UPDATE cycles SET max_price = ? WHERE id = ?`
	_, err := db.conn.Exec(query, maxPrice, id)
	if err != nil {
		return fmt.Errorf("failed to update max_price for cycle %d: %v", id, err)
	}
	return nil
}

func (db *DB) UpdateCycleTargetPrice(id int, targetPrice float64) error {
	query := `UPDATE cycles SET target_price = ? WHERE id = ?`
	_, err := db.conn.Exec(query, targetPrice, id)
	if err != nil {
		return fmt.Errorf("failed to update target_price for cycle %d: %v", id, err)
	}
	return nil
}

// Orders
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

func (db *DB) GetPendingOrders() ([]Order, error) {
	query := `
		SELECT id, external_id, side, amount, price, fees, status, strategy_id, created_at, updated_at 
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
		var strategyId sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
			&order.Status, &strategyId, &order.CreatedAt, &order.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}

		if strategyId.Valid {
			id := int(strategyId.Int64)
			order.StrategyID = &id
		}

		orders = append(orders, order)
	}

	return orders, nil
}

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

func (db *DB) GetOldOrders(olderThan time.Time) ([]Order, error) {
	query := `
		SELECT id, external_id, side, amount, price, fees, status, strategy_id, created_at, updated_at 
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
		var strategyId sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
			&order.Status, &strategyId, &order.CreatedAt, &order.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}

		if strategyId.Valid {
			id := int(strategyId.Int64)
			order.StrategyID = &id
		}

		orders = append(orders, order)
	}

	return orders, nil
}

// Cycles
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

func (db *DB) buildCycleEnhanced(
	id int,
	buyOrderId int64, sellOrderId sql.NullInt64,
	targetPrice, maxPrice float64,
	createdAt, updatedAt time.Time,
) (*CycleEnhanced, error) {
	var cycle CycleEnhanced
	cycle.ID = id
	cycle.MaxPrice = maxPrice
	cycle.TargetPrice = targetPrice
	cycle.CreatedAt = createdAt
	cycle.UpdatedAt = updatedAt

	buyOrder, err := db.GetOrder(int(buyOrderId))
	if err != nil {
		return nil, fmt.Errorf("failed to get buy order: %w", err)
	}
	cycle.BuyOrder = *buyOrder
	if buyOrder.StrategyID == nil {
		return nil, fmt.Errorf("buyOrder.StrategyID should not be nil: %w", err)
	}
	cycle.StrategyID = *buyOrder.StrategyID

	if sellOrderId.Valid {
		sellOrder, err := db.GetOrder(int(sellOrderId.Int64))
		if err != nil {
			return nil, fmt.Errorf("failed to get sell order: %w", err)
		}
		cycle.SellOrder = sellOrder
	}

	// Statut et profit
	if cycle.SellOrder == nil {
		switch cycle.BuyOrder.Status {
		case Pending, Cancelled:
			cycle.Status = "New"
		case Filled:
			cycle.Status = "Open"
		}
	} else {
		switch cycle.SellOrder.Status {
		case Pending, Cancelled:
			cycle.Status = "Pending"
		case Filled:
			cycle.Status = "Completed"
		}
		profit := (cycle.SellOrder.Price - cycle.BuyOrder.Price) * cycle.BuyOrder.Amount
		profit -= cycle.BuyOrder.Fees
		profit -= cycle.SellOrder.Fees
		cycle.Profit = &profit
	}

	// Durée
	var duration time.Duration
	if cycle.SellOrder != nil {
		duration = cycle.SellOrder.CreatedAt.Sub(cycle.CreatedAt)
	} else {
		duration = time.Since(cycle.BuyOrder.CreatedAt)
	}
	cycle.Duration = formatDuration(duration)

	return &cycle, nil
}

func (db *DB) GetCycle(id int) (*CycleEnhanced, error) {
	query := `
		SELECT id, buy_order_id, sell_order_id, target_price, max_price, created_at, updated_at
		FROM cycles
		WHERE id = ?
	`
	row := db.conn.QueryRow(query, id)

	var (
		cycleId     int
		buyOrderId  int64
		sellOrderId sql.NullInt64
		targetPrice float64
		maxPrice    float64
		createdAt   time.Time
		updatedAt   time.Time
	)

	err := row.Scan(&cycleId, &buyOrderId, &sellOrderId, &targetPrice, &maxPrice, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("db.GetCycle() failed to scan cycle row: %w", err)
	}

	return db.buildCycleEnhanced(cycleId, buyOrderId, sellOrderId, targetPrice, maxPrice, createdAt, updatedAt)
}

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

// func (db *DB) GetCycleForBuyOrderPosition(buyOrderPositionId int) (*Cycle, error) {
// 	var cycleId sql.NullInt64
// 	query := `
// 		SELECT c.id
// 		FROM cycles c
// 		JOIN orders bo ON c.buy_order_id = bo.id
// 		WHERE bo.position_id= ?
// 	`
// 	err := db.conn.QueryRow(query, buyOrderPositionId).Scan(&cycleId)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get cycle id for buy order by position: %w", err)
// 	}
// 	if cycleId.Valid {
// 		return db.GetCycle(int(cycleId.Int64))
// 	} else {
// 		return nil, fmt.Errorf("failed to get cycle id for buy order by position: %w", err)
// 	}
// }

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

func (db *DB) GetAllCycles() ([]CycleEnhanced, error) {
	query := `
		SELECT id, buy_order_id, sell_order_id, target_price, max_price, created_at, updated_at
		FROM cycles 
		ORDER BY created_at DESC
	`
	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("db.GetAllCycles() failed to get cycles: %w", err)
	}
	defer rows.Close()

	var cycles []CycleEnhanced
	for rows.Next() {
		var (
			id          int
			buyOrderId  int64
			sellOrderId sql.NullInt64
			targetPrice float64
			maxPrice    float64
			createdAt   time.Time
			updatedAt   time.Time
		)
		err := rows.Scan(&id, &buyOrderId, &sellOrderId, &targetPrice, &maxPrice, &createdAt, &updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan cycle row: %w", err)
		}
		cycle, err := db.buildCycleEnhanced(id, buyOrderId, sellOrderId, targetPrice, maxPrice, createdAt, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to build cycle: %w", err)
		}
		cycles = append(cycles, *cycle)
	}

	return cycles, nil
}

// Méthodes utilitaires
func (db *DB) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Nombre d'ordres en attente
	var pendingCount int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM orders WHERE status = ?`, Pending).Scan(&pendingCount)
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
		SELECT id, external_id, side, amount, price, fees, status, strategy_id, created_at, updated_at 
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
		var strategyId sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
			&order.Status, &strategyId, &order.CreatedAt, &order.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}

		if strategyId.Valid {
			id := int(strategyId.Int64)
			order.StrategyID = &id
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

// // GetPositionWithSellOrders récupère une position avec ses ordres de vente associés
// func (db *DB) GetPositionWithSellOrders(positionId int) (*Position, []Order, error) {
// 	position, err := db.GetPosition(positionId)
// 	if err != nil {
// 		return nil, nil, err
// 	}

// 	query := `
// 		SELECT id, external_id, side, amount, price, fees, status, position_id, strategy_id, created_at, updated_at
// 		FROM orders
// 		WHERE position_id = ? AND side = ?
// 	`
// 	rows, err := db.conn.Query(query, positionId, Sell)
// 	if err != nil {
// 		return position, nil, fmt.Errorf("failed to get sell orders for position: %w", err)
// 	}
// 	defer rows.Close()

// 	var orders []Order
// 	for rows.Next() {
// 		var order Order
// 		var posId, strategyId sql.NullInt64
// 		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
// 			&order.Status, &posId, &order.CreatedAt, &order.UpdatedAt)
// 		if err != nil {
// 			return position, nil, fmt.Errorf("failed to scan sell order: %w", err)
// 		}

// 		if posId.Valid {
// 			id := int(posId.Int64)
// 			order.PositionID = &id
// 		}
// 		if strategyId.Valid {
// 			id := int(strategyId.Int64)
// 			order.StrategyID = &id
// 		}

// 		orders = append(orders, order)
// 	}

// 	return position, orders, nil
// }

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
		SELECT id, external_id, side, amount, price, fees, status, strategy_id, created_at, updated_at 
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
		var strategyId sql.NullInt64
		err := rows.Scan(&order.ID, &order.ExternalID, &order.Side, &order.Amount, &order.Price, &order.Fees,
			&order.Status, &strategyId, &order.CreatedAt, &order.UpdatedAt)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan order: %w", err)
		}

		if strategyId.Valid {
			id := int(strategyId.Int64)
			order.StrategyID = &id
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

// func (db *DB) CreateStrategy(
// 	name, description, algorithmName, cronExpression string,
// 	quoteAmount, profitTarget, trailingStopDelta, sellOffset float64,
// ) (*Strategy, error) {
// 	query := `
// 		INSERT INTO strategies (
// 		  name, description, algorithm_name,
// 		  cron_expression,
// 			quote_amount, profit_target,
// 			trailing_stop_delta, sell_offset
// 		)
// 		VALUES (
// 		  ?, ?, ?,
// 			?,
// 			?, ?,
// 			?, ?
// 		)
// 	`
// 	result, err := db.conn.Exec(query,
// 		name, description, algorithmName,
// 		cronExpression,
// 		quoteAmount, profitTarget,
// 		trailingStopDelta, sellOffset,
// 	)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to create strategy: %w", err)
// 	}

// 	id, err := result.LastInsertId()
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get last insert id: %w", err)
// 	}

// 	return db.GetStrategy(int(id))
// }

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

func (db *DB) UpdateStrategyNextExecution(strategyId int, nextExecution time.Time) error {
	query := `UPDATE strategies SET next_execution_at = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := db.conn.Exec(query, nextExecution, strategyId)
	if err != nil {
		return fmt.Errorf("failed to update strategy next execution: %w", err)
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
// STRATEGY-SPECIFIC METHODS
// ===============================

func (db *DB) GetOpenCyclesForStrategy(strategyId int) ([]CycleEnhanced, error) {
	query := `
		SELECT c.id
		FROM cycles c
		JOIN orders bo ON (bo.id = c.buy_order_id)
		LEFT OUTER JOIN orders so ON (so.id = c.sell_order_id)
		WHERE bo.strategy_id = ? AND (c.sell_order_id IS NULL OR so.status = 'CANCELLED')
		ORDER BY c.created_at DESC
	`
	rows, err := db.conn.Query(query, strategyId)
	if err != nil {
		return nil, fmt.Errorf("failed to get open cycles for strategy: %w", err)
	}
	defer rows.Close()

	var cycles []CycleEnhanced
	for rows.Next() {
		var id int
		err := rows.Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		cycle, err := db.GetCycle(id)
		if err != nil {
			return nil, fmt.Errorf("failed to get cycle: %w", err)
		}

		cycles = append(cycles, *cycle)
	}

	return cycles, nil
}

func (db *DB) CountActiveOrdersForStrategy(strategyId int) (int, error) {
	query := `SELECT COUNT(*) FROM orders WHERE strategy_id = ? AND status = ?`
	var count int
	err := db.conn.QueryRow(query, strategyId, Pending).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count active orders for strategy: %w", err)
	}
	return count, nil
}

func (db *DB) CountOrdersForStrategy(strategyId int) (int, error) {
	query := `SELECT COUNT(*) FROM orders WHERE strategy_id = ?`
	var count int
	err := db.conn.QueryRow(query, strategyId).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count orders for strategy: %w", err)
	}
	return count, nil
}

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

	// Count positions for this strategy
	var positionsCount int
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM positions WHERE strategy_id = ?`, strategyId).Scan(&positionsCount)
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

	// Insert strategy with hardcoded defaults (for cmd tool compatibility)
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

func (db *DB) CreateStrategyFromWeb(name, description, algorithm, cron string, enabled bool,
	quoteAmount, profitTarget, trailingStopDelta, sellOffset float64,
	rsiThreshold *float64, rsiPeriod *int, rsiTimeframe string,
	macdFastPeriod, macdSlowPeriod, macdSignalPeriod int, macdTimeframe string,
	bbPeriod int, bbMultiplier float64, bbTimeframe string,
	volatilityPeriod *int, volatilityAdjustment *float64, volatilityTimeframe string,
	concurrentOrders int) error {

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

	// Insert strategy with all web form parameters
	query := `
		INSERT INTO strategies (
			name, description, enabled, algorithm_name, cron_expression, quote_amount,
			rsi_threshold, rsi_period, rsi_timeframe,
			macd_fast_period, macd_slow_period, macd_signal_period, macd_timeframe,
			bb_period, bb_multiplier, bb_timeframe,
			profit_target, trailing_stop_delta, sell_offset,
			volatility_period, volatility_adjustment, volatility_timeframe,
			max_concurrent_orders
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = db.conn.Exec(query, name, description, enabled, algorithm, cron, quoteAmount,
		rsiThreshold, rsiPeriod, rsiTimeframe,
		macdFastPeriod, macdSlowPeriod, macdSignalPeriod, macdTimeframe,
		bbPeriod, bbMultiplier, bbTimeframe,
		profitTarget, trailingStopDelta, sellOffset,
		volatilityPeriod, volatilityAdjustment, volatilityTimeframe,
		concurrentOrders)
	if err != nil {
		return fmt.Errorf("failed to create strategy: %w", err)
	}

	return nil
}

func (db *DB) UpdateStrategy(id int, name, description, algorithm, cron string, enabled bool,
	quoteAmount, profitTarget, trailingStopDelta, sellOffset float64,
	rsiThreshold *float64, rsiPeriod *int, rsiTimeframe string,
	macdFastPeriod, macdSlowPeriod, macdSignalPeriod int, macdTimeframe string,
	bbPeriod int, bbMultiplier float64, bbTimeframe string,
	volatilityPeriod *int, volatilityAdjustment *float64, volatilityTimeframe string,
	maxConcurrentOrders int) error {

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
			max_concurrent_orders = ?,
			updated_at = CURRENT_TIMESTAMP
    WHERE id = ?
	`

	_, err := db.conn.Exec(query, name, description, algorithm, cron, enabled,
		quoteAmount, profitTarget, trailingStopDelta, sellOffset,
		rsiThreshold, rsiPeriod, rsiTimeframe,
		macdFastPeriod, macdSlowPeriod, macdSignalPeriod, macdTimeframe,
		bbPeriod, bbMultiplier, bbTimeframe,
		volatilityPeriod, volatilityAdjustment, volatilityTimeframe,
		maxConcurrentOrders, id)

	return err
}

func (db *DB) ToggleStrategyEnabled(id int) error {
	query := `UPDATE strategies SET enabled = NOT enabled, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := db.conn.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to toggle strategy enabled status: %w", err)
	}
	return nil
}

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

func (db *DB) DeleteNonLegacyStrategies() (int64, error) {
	result, err := db.conn.Exec(`DELETE FROM strategies WHERE id != 1`)
	if err != nil {
		return 0, fmt.Errorf("failed to delete strategies: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}

// Methods already exist above, removing duplicates
