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

// Order side and status types
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

type CycleStatus string

const (
	New       CycleStatus = "New"
	Open      CycleStatus = "Open"
	Running   CycleStatus = "Running"
	Completed CycleStatus = "Completed"
)

// Core data structures
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
	StrategyID int         `json:"strategy_id"`
	Status     CycleStatus `json:"status"`
	Profit     *float64    `json:"profit,omitempty"`
	Duration   string      `json:"duration"`
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

// Helper structures for optimized cycle scanning
type OrderScanResult struct {
	ID         sql.NullInt64
	StrategyID sql.NullInt64
	ExternalID sql.NullString
	Side       sql.NullString
	Amount     sql.NullString
	Price      sql.NullString
	Fees       sql.NullString
	Status     sql.NullString
	CreatedAt  sql.NullTime
	UpdatedAt  sql.NullTime
}

type CycleScanResult struct {
	ID          int
	TargetPrice float64
	MaxPrice    float64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	BuyOrder    OrderScanResult
	SellOrder   OrderScanResult
}

// Main database structure
type DB struct {
	conn *sql.DB
}

// Database migrations
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
				ADD COLUMN max_price REAL DEFAULT 0.0;
			ALTER TABLE cycles 
				ADD COLUMN target_price REAL DEFAULT 0.0;

			UPDATE cycles SET 
			  max_price = (
					SELECT p.max_price 
					FROM positions p 
					JOIN orders bo ON p.id = bo.position_id 
					WHERE bo.id = cycles.buy_order_id
				);
			UPDATE cycles SET
			  max_price = 0.0
			WHERE max_price IS NULL;

			UPDATE cycles SET
				target_price = (
					SELECT p.target_price 
					FROM positions p 
					JOIN orders bo ON p.id = bo.position_id 
					WHERE bo.id = cycles.buy_order_id
				);
			UPDATE cycles SET
			  target_price = 0.0
			WHERE target_price IS NULL;				
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

// NewDB creates a new database connection and applies migrations
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

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// applyMigrations applies all pending database migrations
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
