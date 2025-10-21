# Available Binaries

The Simple Trading Bot provides multiple executables for different purposes, from core trading operations to development and administration tools.

## 🏗️ Build Process

All binaries are built from the `cmd/` directory and output to `bin/`:

```bash
# Build all binaries in release mode
make release

# Build specific binary
make build-bot

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
```

**Configuration:**
- Requires `config.yml` with exchange and trading parameters
- Requires `.env` with exchange API credentials
- Requires `../.env` with CUSTOMER_ID and BOT_RELOAD_TOKEN
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
./bin/admin --root storage/mexc --cmd strategies

# Show recent orders
./bin/admin --root storage/mexc --cmd orders

# Database statistics
./bin/admin --root storage/mexc --cmd stats
```

**Commands:**
- `orders` - Order history and status tracking
- `cycles` - Trading cycle analysis
- `stats` - Database and performance statistics
- `export` - Export database data

## 🧪 Testing & Development Tools

### test (`cmd/test/main.go`)

**Purpose**: Integration testing

**Key Features:**
- Check configuration and exchange API connectivity
- Place and cancel a buy order
- Place and cancel a sell order

**Usage:**
```bash
# Run integration tests
./bin/test --root storage/mexc
```

### rsi (`cmd/rsi/main.go`)

**Purpose**: Compute RSI (Relative Strength Index)

**Key Features:**
- Compute the RSI using the same code as the bot

**Usage:**
```bash
# Compute the RSI
./bin/rsi --root storage/mexc
```

### volatility (`cmd/volatility/main.go`)

**Purpose**: Compute Volatility

**Key Features:**
- Compute Volatility using the same code as the bot

**Usage:**
```bash
# Compute the volatility
./bin/volatility --root storage/mexc
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
├── config.yml         # Main configuration (trading parameters)
├── .env               # Exchange credentials
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