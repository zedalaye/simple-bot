# Database Schema & Migrations

The Simple Trading Bot uses SQLite as its primary database with an incremental migration system to ensure safe schema evolution and backward compatibility.

## 🗄️ Database Overview

- **Engine**: SQLite 3 with WAL mode for concurrent access
- **Location**: `storage/{exchange}/db/bot.db` (one per exchange instance)
- **Migrations**: Incremental SQL scripts with rollback support
- **Backup**: Automated via Docker Compose commands

## 📊 Core Tables

### strategies

Stores trading strategy definitions and their execution configuration.

```sql
CREATE TABLE strategies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    description TEXT,
    algorithm_name TEXT NOT NULL,           -- Algorithm identifier (rsi_dca, macd_cross)
    enabled BOOLEAN DEFAULT 1,
    cron_expression TEXT NOT NULL,          -- Cron schedule for buy execution
    quote_amount REAL NOT NULL,             -- Base amount for trades
    max_concurrent_cycles INTEGER DEFAULT 1,-- Max simultaneous cycles (0 = unlimited)

    -- Performance tracking
    total_orders INTEGER DEFAULT 0,
    successful_orders INTEGER DEFAULT 0,
    total_profit REAL DEFAULT 0.0,

    -- Timestamps
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_executed_at DATETIME NULL,
    next_execution_at DATETIME NULL
);

-- Indexes
CREATE INDEX idx_strategies_enabled ON strategies(enabled);
CREATE INDEX idx_strategies_next_execution ON strategies(next_execution_at);
CREATE INDEX idx_strategies_algorithm ON strategies(algorithm_name);
```

**Key Fields:**
- `algorithm_name`: References registered algorithms in the codebase
- `cron_expression`: Standard cron format (e.g., `"*/5 * * * *"` for every 5 minutes)
- `max_concurrent_cycles`: Safety limit to prevent over-trading

### orders

Tracks all exchange orders with their lifecycle and metadata.

```sql
CREATE TABLE orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    external_id TEXT UNIQUE,                -- Exchange order ID
    side TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    amount REAL NOT NULL,
    price REAL NOT NULL,
    fees REAL DEFAULT 0.0,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'filled', 'cancelled', 'expired')),

    strategy_id INTEGER REFERENCES strategies(id) ON DELETE SET NULL,
    cycle_id INTEGER REFERENCES cycles(id) ON DELETE SET NULL,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    filled_at DATETIME NULL
);

-- Indexes
CREATE INDEX idx_orders_external_id ON orders(external_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_strategy ON orders(strategy_id);
CREATE INDEX idx_orders_cycle ON orders(cycle_id);
CREATE INDEX idx_orders_created_at ON orders(created_at);
```

**Order Lifecycle:**
1. `pending` → Order submitted to exchange
2. `filled` → Order completely executed
3. `cancelled` → Order cancelled by user/system
4. `expired` → Order expired on exchange

### cycles

Manages trading cycles from buy to sell completion.

```sql
CREATE TABLE cycles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    strategy_id INTEGER REFERENCES strategies(id) ON DELETE SET NULL,

    -- Order relationships
    buy_order_id INTEGER REFERENCES orders(id) ON DELETE SET NULL,
    sell_order_id INTEGER REFERENCES orders(id) ON DELETE SET NULL,

    -- Price tracking
    max_price REAL DEFAULT 0.0,             -- Highest price during cycle
    target_price REAL,                      -- Profit target from buy signal

    -- Status and timestamps
    status TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'closed', 'cancelled')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    closed_at DATETIME NULL
);

-- Indexes
CREATE INDEX idx_cycles_strategy ON cycles(strategy_id);
CREATE INDEX idx_cycles_status ON cycles(status);
CREATE INDEX idx_cycles_buy_order ON cycles(buy_order_id);
CREATE INDEX idx_cycles_sell_order ON cycles(sell_order_id);
```

**Cycle States:**
- `open` → Buy order filled, waiting for sell
- `closed` → Sell order filled, cycle complete
- `cancelled` → Cycle terminated without completion

### candles

Caches market data for performance and indicator calculations.

```sql
CREATE TABLE candles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    exchange TEXT NOT NULL,
    pair TEXT NOT NULL,
    timeframe TEXT NOT NULL,                -- e.g., '5m', '1h', '1d'

    timestamp INTEGER NOT NULL,             -- Unix timestamp
    open REAL NOT NULL,
    high REAL NOT NULL,
    low REAL NOT NULL,
    close REAL NOT NULL,
    volume REAL NOT NULL,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(exchange, pair, timeframe, timestamp)
);

-- Indexes
CREATE INDEX idx_candles_exchange_pair ON candles(exchange, pair);
CREATE INDEX idx_candles_timeframe_timestamp ON candles(timeframe, timestamp);
CREATE INDEX idx_candles_timestamp ON candles(timestamp);
```

**Usage:**
- Stores ~200 recent candles per pair/timeframe
- Supports indicator calculations without repeated API calls
- Enables backtesting and strategy validation

### migrations

Tracks database schema evolution.

```sql
CREATE TABLE migrations (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## 🔄 Migration System

### Migration Files

Located in `internal/core/database/datanbase.go`, each migration is a numbered and is associated with a SQLite script:

```
1: init schema
2: add max price
3: add order fees
4: create cycles
5: add target price to positions
6: create candles
7: create strategies
8: add missing strategies ID columns and indexes
9: create legacy strategy with old default configuration values
10: migrate max_price and target_price to cycles
11: decouple orders and positions
12: decouples cycles from strategies
12: remove positions
```

### Migration Process

```go
// Migration execution in database.go
func (db *DB) runMigrations() error {
    migrations, err := db.getPendingMigrations()
    if err != nil {
        return err
    }

    for _, migration := range migrations {
        if err := db.executeMigration(migration); err != nil {
            return fmt.Errorf("failed to execute migration %s: %w", migration.Name, err)
        }
        db.recordMigration(migration)
    }

    return nil
}
```

### Safe Migration Practices

- **Backward Compatible**: ALTER TABLE operations preserve existing data
- **Transactional**: Each migration runs in a transaction
- **Idempotent**: Can be re-run safely
- **Rollback Ready**: Schema changes are reversible

## 🔗 Table Relationships

```
strategies (1) ──── (N) orders
    │                      │
    │                      │
    └── cycles (1) ──── (N) orders
           │
           │
        candles (independent cache)
```

### Foreign Key Constraints

- `orders.strategy_id` → `strategies.id`
- `orders.cycle_id` → `cycles.id`
- `cycles.strategy_id` → `strategies.id`
- `cycles.buy_order_id` → `orders.id`
- `cycles.sell_order_id` → `orders.id`

## 📈 Performance Optimizations

### Indexes

Critical indexes for common query patterns:

```sql
-- Strategy scheduling
CREATE INDEX idx_strategies_next_execution ON strategies(next_execution_at);

-- Order management
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_strategy ON orders(strategy_id);

-- Cycle tracking
CREATE INDEX idx_cycles_status ON cycles(status);
CREATE INDEX idx_cycles_strategy ON cycles(strategy_id);

-- Market data queries
CREATE INDEX idx_candles_timestamp ON candles(timestamp);
CREATE INDEX idx_candles_exchange_pair_timeframe ON candles(exchange, pair, timeframe);
```

### Query Patterns

**Common Queries Optimized:**

```sql
-- Get pending orders for monitoring
SELECT * FROM orders WHERE status = 'pending' ORDER BY created_at DESC;

-- Get open cycles for strategy
SELECT * FROM cycles WHERE strategy_id = ? AND status = 'open';

-- Get recent candles for indicators
SELECT * FROM candles WHERE exchange = ? AND pair = ? AND timeframe = ?
ORDER BY timestamp DESC LIMIT 200;
```

## 🔧 Database Operations

### Error Handling

- **Constraint Violations**: Foreign key and unique constraints
- **Connection Issues**: Automatic retry with exponential backoff
- **Migration Failures**: Transaction rollback and error reporting

## 📊 Monitoring & Maintenance

### Health Checks

```sql
-- Verify database connectivity
SELECT 1;

-- Check migration status
SELECT id, name, applied_at FROM migrations ORDER BY id DESC;

-- Monitor table sizes
SELECT name, sql FROM sqlite_master WHERE type='table';
```

### Backup Strategy

```bash
# Automated backup via Docker
./simple-bot all backup

# Manual backup
sqlite3 storage/mexc/db/bot.db ".backup backup.db"
```

### Performance Monitoring

- **Query Execution Time**: Logged for slow queries (>100ms)
- **Connection Pool Stats**: Active/idle connections
- **Table Size Monitoring**: Automatic alerts for large tables

This database design provides a robust foundation for trading operations with proper data integrity, performance optimization, and maintainability through incremental migrations.