# Available Binaries

The Simple Trading Bot provides multiple executables for different purposes, from core trading operations to development and administration tools.

## 🏗️ Build Process

All binaries are built from the `cmd/` directory and output to `bin/`:

```bash
# Build all binaries
make

# Build specific binary
make bin/bot

# Build with Docker
make build-image
```

## 🤖 Core Trading Binaries

### bot (`cmd/bot/main.go`)

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
./bin/bot --root storage/mexc

# Run for Hyperliquid
./bin/bot --root storage/hl

# With custom config
./bin/bot --root storage/mexc --config custom-config.yml
```

**Configuration:**
- Requires `config.yml` with trading parameters
- Requires `.env` with exchange API credentials
- Optional: `../.env.tg` for Telegram notifications

**Processes Started:**
- Main bot loop with price monitoring
- Strategy scheduler with cron jobs
- Order status checker
- Telegram notification handler

### web (`cmd/web/main.go`)

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
./bin/web --root storage/mexc

# Custom port
./bin/web --root storage/mexc --port 8081

# With reload token for API access
./bin/web --root storage/mexc --reload-token your-token
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

### admin (`cmd/admin/main.go`)

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
# Show help
./bin/admin --help

# List all strategies
./bin/admin --root storage/mexc strategies

# Show recent orders
./bin/admin --root storage/mexc orders --limit 10

# Display cycles with profit analysis
./bin/admin --root storage/mexc cycles --status closed

# Database statistics
./bin/admin --root storage/mexc stats
```

**Commands:**
- `strategies` - List and manage trading strategies
- `orders` - Order history and status tracking
- `cycles` - Trading cycle analysis
- `stats` - Database and performance statistics
- `migrations` - Migration status and history

## 🧪 Testing & Development Tools

### test (`cmd/test/main.go`)

**Purpose**: Integration testing and simulation tool for strategy validation.

**Key Features:**
- End-to-end trading simulation
- Strategy backtesting against historical data
- Performance benchmarking
- Risk analysis and validation
- Paper trading mode for safe testing

**Usage:**
```bash
# Run integration tests
./bin/test --root storage/mexc

# Backtest strategy against historical data
./bin/test --root storage/mexc --backtest --strategy rsi_dca --days 30

# Paper trading mode
./bin/test --root storage/mexc --paper-trade --balance 1000
```

**Testing Scenarios:**
- Market condition simulation
- Strategy parameter optimization
- Risk management validation
- Performance regression testing

### rsi (`cmd/rsi/main.go`)

**Purpose**: RSI (Relative Strength Index) strategy development and testing tool.

**Key Features:**
- RSI indicator calculation and visualization
- Overbought/oversold signal generation
- Strategy parameter tuning
- Historical RSI analysis
- Integration testing for RSI-based strategies

**Usage:**
```bash
# Analyze RSI for a trading pair
./bin/rsi --root storage/mexc --pair BTC/USDC

# Test RSI strategy parameters
./bin/rsi --root storage/mexc --period 14 --overbought 70 --oversold 30

# Generate RSI signals from historical data
./bin/rsi --root storage/mexc --backtest --days 7
```

### volatility (`cmd/volatility/main.go`)

**Purpose**: Volatility-based strategy development and analysis tool.

**Key Features:**
- Volatility calculation using ATR (Average True Range)
- Bollinger Bands analysis
- Volatility breakout detection
- Risk assessment based on market volatility
- Strategy optimization for different volatility regimes

**Usage:**
```bash
# Analyze volatility for trading pair
./bin/volatility --root storage/mexc --pair BTC/USDC

# Calculate ATR for risk management
./bin/volatility --root storage/mexc --atr-period 14

# Test volatility-based entry signals
./bin/volatility --root storage/mexc --breakout-threshold 2.0
```

### strategy-demo (`cmd/strategy-demo/main.go`)

**Purpose**: Generic strategy demonstration and prototyping tool.

**Key Features:**
- Framework for testing custom strategies
- Indicator combination testing
- Strategy performance comparison
- Parameter optimization framework
- Code generation for new strategies

**Usage:**
```bash
# Run strategy demonstration
./bin/strategy-demo --root storage/mexc

# Test custom strategy logic
./bin/strategy-demo --root storage/mexc --strategy custom

# Generate strategy template
./bin/strategy-demo --template --name my_strategy
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
mexc-bot      → ./bin/bot (MEXC)
mexc-web      → ./bin/web (MEXC)
hl-bot        → ./bin/bot (Hyperliquid)
hl-web        → ./bin/web (Hyperliquid)
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
├── config.yml          # Main configuration (trading parameters)
├── .env                # Exchange credentials
├── db/bot.db          # SQLite database
└── ../.env.tg         # Telegram notifications (optional)
```

### Environment Variables

**Common:**
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