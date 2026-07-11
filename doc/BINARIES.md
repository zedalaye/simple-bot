# Available Commands

The Simple Trading Bot ships as a **single binary** — `simple-bot` — that bundles every
command as a subcommand (core trading, administration, development tools). The common code
is therefore compiled and deployed once, and a single artifact is shipped.

```
simple-bot [--root DIR] <command> [command options...]
```

`--root` is a **global** flag handled by the dispatcher: it must come **before** the command
name. The dispatcher `chdir`s into that instance directory once (so each command reads the
right `.env`, credentials and database), then delegates. Offline analysis commands
(`backtest`, `patternscan`) also honour `--root`: they read the instance database resolved
from `DB_PATH`.

Available commands: `bot`, `web`, `admin`, `backtest`, `patternscan`, `order`, `rsi`,
`volatility`, `test`.

## 🏗️ Build Process

The binary is built from `cmd/simple-bot` (each command lives in `internal/cli/<name>cli`)
and output to `bin/simple-bot`:

```bash
# Build the single binary in release mode
make release

# Build the single binary
make build-simple-bot

# Build with Docker
make build-image
```

## 🤖 Core Trading Binaries

### bot (`internal/cli/botcli`)

**Purpose**: Main trading engine that executes strategies and monitors markets.

**Key Features:**
- Runs trading strategies according to their cron schedules
- Monitors market prices every 5 minutes
- Executes buy orders based on strategy signals
- Checks for sell opportunities on open positions
- Handles order lifecycle (pending → filled/cancelled)
- Updates cycle max prices for trailing stops
- Sends Telegram notifications for significant events

**Usage:**
```bash
# Run for MEXC exchange
./bin/simple-bot --root storage/mexc bot

# Run for Hyperliquid
./bin/simple-bot --root storage/hl bot
```

**Configuration:**
- Requires `.env` with exchange config and API credentials (see `.env.example`)
- Requires `../.env` with CUSTOMER_ID and BOT_RELOAD_TOKEN
- Optional: `../.env.tg` for Telegram notifications

**Processes Started:**
- Main bot loop with price monitoring
- Strategy scheduler with cron jobs
- Order status checker
- Telegram notification handler

### web (`internal/cli/webcli`)

**Purpose**: HTTP server providing REST API and web interface for monitoring and management.

**Key Features:**
- REST API for programmatic access to bot data
- Web dashboard for real-time monitoring
- Strategy management (CRUD operations)
- Order and cycle tracking
- Performance statistics and charts
- Configuration management interface

**Usage:**
```bash
# Start web server for MEXC
./bin/simple-bot --root storage/mexc web

# Custom port
./bin/simple-bot --root storage/mexc web --port 8081
```

**API Endpoints:**
- `GET /api/strategies` - List/manage strategies
- `GET /api/orders` - Order history and status
- `GET /api/cycles` - Trading cycle information
- `GET /api/stats` - Performance metrics
- `GET /` - Web dashboard

**Security:**
- API token authentication required for modifications
- Configurable via `BOT_RELOAD_TOKEN` environment variable

## 🛠️ Administration & Management

### admin (`internal/cli/admincli`)

**Purpose**: Database administration and inspection tool.

**Key Features:**
- List all strategies with their status and performance
- Display order history with filtering options
- Show trading cycles and profit/loss analysis
- Database maintenance and statistics
- Migration status checking
- Bulk operations and data export

**Usage:**
```bash
# List all strategies
./bin/simple-bot --root storage/mexc admin --cmd strategies

# Show recent orders
./bin/simple-bot --root storage/mexc admin --cmd orders

# Database statistics
./bin/simple-bot --root storage/mexc admin --cmd stats
```

**Commands:**
- `orders` - Order history and status tracking
- `cycles` - Trading cycle analysis
- `stats` - Database and performance statistics
- `export` - Export database data

## 🧪 Testing & Development Tools

### test (`internal/cli/testcli`)

**Purpose**: Integration testing

**Key Features:**
- Check configuration and exchange API connectivity
- Place and cancel a buy order
- Place and cancel a sell order

**Usage:**
```bash
# Run integration tests
./bin/simple-bot --root storage/mexc test
```

### rsi (`internal/cli/rsicli`)

**Purpose**: Compute RSI (Relative Strength Index)

**Key Features:**
- Compute the RSI using the same code as the bot

**Usage:**
```bash
# Compute the RSI
./bin/simple-bot --root storage/mexc rsi
```

### volatility (`internal/cli/volatilitycli`)

**Purpose**: Compute Volatility

**Key Features:**
- Compute Volatility using the same code as the bot

**Usage:**
```bash
# Compute the volatility
./bin/simple-bot --root storage/mexc volatility
```

### backtest (`internal/cli/backtestcli`)

**Purpose**: Replay an `rsi_dca` strategy over historical candles to evaluate
and optimise parameters.

**Key Features:**
- Reuses the real decision code (`internal/algorithms`) and indicator math
  (`internal/market`) via the shared `IndicatorCalculator` interface — faithful
  to production
- No look-ahead: decisions at candle close, orders fill on later candles
- Parameter grid sweep over RSI timeframe, threshold, profit target and buy
  interval; reports buys/day, cycles/day, capital, inventory, P&L
- Works on the instance database resolved from `--root` (`DB_PATH`), no network
  access. The `--pair` defaults to the instance's `TRADING_PAIR`.

**Usage:**
```bash
# Reproduce an existing strategy
./bin/simple-bot --root storage/mexc backtest --strategy-id 1

# Sweep parameters
./bin/simple-bot --root storage/mexc backtest --rsi-tf 15m,1h \
  --rsi-threshold 40,45,50 --profit 1,2 --interval 21600,43200,86400 --vol-adj 0
```

### patternscan (`internal/cli/patternscancli`)

**Purpose**: Measure the predictive power of bullish reversal candle patterns over
the stored history (as-of replay, forward returns vs a random-entry baseline).

**Key Features:**
- As-of evaluation (no look-ahead), forward returns over configurable horizons
- Optional context/volume filters and a "confirmations" dissection section
- Works on the instance database resolved from `--root`; `--pair` defaults to the
  instance's `TRADING_PAIR`

**Usage:**
```bash
# Scan 4h candles of the instance pair
./bin/simple-bot --root storage/mexc patternscan --tf 4h
```

## 🐳 Docker Integration

All binaries are available through Docker Compose with multi-exchange support:

```bash
# Build all images
make build-image

# Run MEXC bot and web interface
./simple-bot mexc up

# Run Hyperliquid services
./simple-bot hl up

# Run all exchanges
./simple-bot all up

# View logs
./simple-bot mexc logs

# Stop services
./simple-bot all down

# Backup databases
./simple-bot all backup
```

### Docker Service Mapping

```
bot-mexc      → /app/simple-bot bot (MEXC)
webui-mexc    → /app/simple-bot web (MEXC)
bot-hl        → /app/simple-bot bot (Hyperliquid)
webui-hl      → /app/simple-bot web (Hyperliquid)
```

## 🔧 Binary Dependencies

### Runtime Requirements

- **bot**: Exchange API access, database write access
- **web**: Database read access, network port availability
- **admin**: Database read access
- **test**: Database access, exchange API (for live testing)
- **Development tools**: Database access, may require exchange API

### Configuration Files

Each binary expects specific configuration in the `--root` directory:

```
storage/{exchange}/
├── .env               # All config + exchange credentials (see .env.example)
├── db/bot.db          # SQLite database
├── ../.env            # CUSTOMER_ID and BOT_RELOAD_TOKEN
└── ../.env.tg         # Telegram notifications (optional)
```

### Environment Variables

**Common:**
- `CUSTOMER_ID` - Your Cryptomancien Premium Customer Id
- `BOT_RELOAD_TOKEN` - API authentication token

**MEXC:**
- `API_KEY` - MEXC API key
- `SECRET` - MEXC API secret

**Hyperliquid:**
- `WALLET_ADDRESS` - Arbitrum wallet address
- `PRIVATE_KEY` - Trading wallet private key

**Telegram (Optional):**
- `TELEGRAM_BOT_TOKEN` - Bot token from BotFather
- `TELEGRAM_CHAT_ID` - Target chat ID

## 📊 Resource Usage

### Memory Footprint
- **bot**: ~50-100MB (depends on candle cache size)
- **web**: ~30-50MB (serving static assets + API)
- **admin**: ~20-30MB (database queries)
- **Development tools**: ~30-60MB

### CPU Usage
- **bot**: Variable (peaks during strategy execution and price checks)
- **web**: Low (mostly I/O bound)
- **admin**: Low (query execution)
- **test**: Variable (depends on simulation complexity)

### Disk Usage
- **Database**: ~10-100MB per exchange (depends on trade history)
- **Candle cache**: ~5-20MB (200 candles × multiple pairs)
- **Logs**: Configurable retention

This comprehensive set of binaries provides everything needed for development, testing, deployment, and operation of the trading bot across different use cases and environments.