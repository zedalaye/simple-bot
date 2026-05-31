# System Architecture

The Simple Trading Bot is a modular, multi-exchange trading system designed for automated cryptocurrency trading with strategy-based execution and comprehensive monitoring capabilities.

## 🏛️ High-Level Overview

```
┌─────────────────┐    ┌───────────────────┐    ┌─────────────────┐
│   Exchanges     │◄──►│  Trading Bot      │◄──►│   Database      │
│                 │    │                   │    │                 │
│ • MEXC          │    │ • Strategy Engine │    │ • SQLite        │
│ • Hyperliquid   │    │ • Price Monitor   │    │ • Migrations    │
│ • CCXT Unified  │    │ • Order Manager   │    │ • Market Data   │
└─────────────────┘    └───────────────────┘    └─────────────────┘
                                ▲
                                │
                       ┌───────────────────┐
                       │   Web Interface   │
                       │                   │
                       │ • REST API        │
                       │ • Monitoring UI   │
                       │ • Strategy Mgmt   │
                       └───────────────────┘
```

## 🧩 Core Components

### 1. Bot Engine (`internal/bot/`)

The central orchestrator that manages the entire trading lifecycle.

**Key Responsibilities:**
- **Price Monitoring**: Checks market prices every 5 minutes
- **Strategy Execution**: Coordinates buy/sell strategy execution
- **Order Lifecycle**: Monitors pending orders and handles fills/cancellations
- **Health Monitoring**: System status and error handling

**Main Components:**
- `Bot` struct: Main bot instance with exchange and database connections
- `handlePriceCheck()`: Periodic price monitoring and sell strategy execution
- `handleOrderCheck()`: Order status monitoring and lifecycle management

### 2. Strategy Scheduler (`internal/scheduler/`)

Manages the execution timing and orchestration of trading strategies.

**Key Responsibilities:**
- **Cron Scheduling**: Executes buy strategies based on cron expressions
- **Strategy Validation**: Ensures strategies are properly configured
- **Algorithm Registry**: Manages available trading algorithms
- **Premium Checks**: Validates subscription status

**Main Components:**
- `StrategyScheduler`: Cron-based job scheduler using gocron
- `StrategyManager`: Executes individual strategies with market data
- `ExecuteBuyStrategy()`: Handles buy-side strategy execution
- `ExecuteSellStrategy()`: Handles sell-side strategy execution

### 3. Trading Algorithms (`internal/algorithms/`)

Pluggable strategy implementations that define trading logic.

**Key Responsibilities:**
- **Signal Generation**: Analyzes market conditions to generate buy/sell signals
- **Risk Management**: Implements position sizing and stop-loss logic
- **Parameter Validation**: Ensures strategy configuration is valid

**Built-in Algorithms:**
- **RSI_DCA**: RSI-based dollar-cost averaging
- **MACD_Cross**: MACD crossover strategy

**Algorithm Interface:**
```go
type Algorithm interface {
    Name() string
    Description() string
    ShouldBuy(ctx TradingContext, strategy Strategy) (BuySignal, error)
    ShouldSell(ctx TradingContext, cycle Cycle, strategy Strategy) (SellSignal, error)
    ValidateConfig(strategy Strategy) error
    RequiredIndicators() []string
}
```

### 4. Market Data System (`internal/market/`)

Handles market data collection, caching, and technical analysis.

**Key Responsibilities:**
- **Candle Collection**: Fetches and stores OHLCV data from exchanges
- **Indicator Calculation**: Computes technical indicators (RSI, MACD, etc.)
- **Precision Handling**: Manages price/amount precision for different pairs
- **Performance Optimization**: Caches data to reduce API calls

**Main Components:**
- `MarketDataCollector`: Manages candle collection and storage
- `Calculator`: Performs technical analysis calculations
- `MarketPrecision`: Handles exchange-specific precision requirements

### 5. Database Layer (`internal/core/database/`)

SQLite-based data persistence with incremental migrations.

**Key Responsibilities:**
- **Data Persistence**: Stores all trading data (orders, positions, strategies)
- **Migration Management**: Handles schema updates safely
- **Query Optimization**: Provides efficient data access patterns
- **Data Integrity**: Maintains referential integrity and constraints

### 6. Web Interface (`internal/web/`)

HTTP API and web UI for monitoring and management.

**Key Responsibilities:**
- **REST API**: Provides programmatic access to bot data
- **Web Dashboard**: User interface for monitoring and configuration
- **Real-time Updates**: Live statistics and order status
- **Strategy Management**: CRUD operations for trading strategies

## 🔄 Data Flow Architecture

### Trading Cycle Lifecycle

```
1. STRATEGY EVALUATION
   ├── Cron trigger → StrategyScheduler
   ├── Market data fetch → TradingContext
   └── Algorithm.ShouldBuy() → BuySignal

2. ORDER EXECUTION
   ├── Signal validation → Balance check
   ├── Exchange.PlaceLimitBuyOrder() → Order
   └── Database.CreateOrder() → Persist

3. POSITION MONITORING
   ├── Price updates every 5min → Cycle max price
   ├── Algorithm.ShouldSell() → SellSignal
   └── Exchange.PlaceLimitSellOrder() → Close position

4. PROFIT REALIZATION
   ├── Order fill detection → Status update
   ├── Profit calculation → Statistics update
   └── Telegram notification → User alert
```

### Component Communication

```
User Request → Web Handler → Database Query → JSON Response
Strategy Cron → Scheduler → Algorithm → Exchange API → Database
Price Update → Bot Monitor → Sell Check → Exchange API → Database
```

## 🗃️ Database Schema Overview

### Core Tables

```sql
strategies    # Trading strategies with cron schedules
├── id, name, config, enabled, cron_expression
└── performance metrics, execution timestamps

orders        # Exchange orders (buy/sell)
├── id, external_id, side, amount, price, fees
├── status (pending/filled/cancelled), strategy_id
└── timestamps, exchange metadata

cycles        # Trading cycles (buy → sell pairs)
├── id, buy_order_id, sell_order_id, strategy_id
├── max_price tracking, profit calculation
└── position lifecycle management

candles       # Market data cache (OHLCV)
├── timestamp, open, high, low, close, volume
├── pair, timeframe, exchange
└── indicator calculations cache
```

### Key Relationships

- **Strategy → Orders**: One-to-many (strategy can have multiple orders)
- **Orders → Cycles**: Buy orders start cycles, sell orders complete them
- **Cycles → Strategy**: All cycles are associated with their originating strategy
- **Candles**: Independent market data cache for performance

## 🔧 Configuration Architecture

### Multi-Exchange Support

```
storage/
├── mexc/           # MEXC exchange instance
│   ├── .env        # All config + API credentials (see .env.example)
│   └── db/bot.db   # SQLite database
│
├── hl/             # Hyperliquid exchange instance
│   ├── .env        # All config + wallet credentials (see .env.example)
│   └── db/bot.db   # SQLite database
│
├── .env            # Common secrets and credentials
└── .env.tg         # Telegram notifications (shared)
```

### Strategy Configuration

Strategies are defined with flexible JSON configuration:

```json
{
  "name": "RSI DCA Strategy",
  "algorithm": "rsi_dca",
  "cronExpression": "*/5 * * * *",
  "parameters": {
    "rsiPeriod": 14,
    "rsiOverbought": 70,
    "rsiOversold": 30,
    "dcaAmount": 100,
    "targetProfit": 0.05
  }
}
```

## 🚀 Deployment Architecture

### Docker Compose Setup

```yaml
services:
  mexc-bot:      # MEXC trading instance
  mexc-web:      # MEXC web interface
  hl-bot:        # Hyperliquid trading instance
  hl-web:        # Hyperliquid web interface
```

### Process Architecture

- **Bot Process**: Core trading engine with strategy execution
- **Web Process**: HTTP server for API and UI access
- **Database**: SQLite files (one per exchange instance)
- **Shared Config**: Telegram notifications and common settings

## 🔒 Security Considerations

- **API Keys**: Encrypted storage in environment files
- **Database**: Local SQLite with file system permissions
- **Network**: HTTPS for web interface, secure exchange connections
- **Access Control**: API token authentication for web endpoints

## 📊 Monitoring & Observability

- **Logs**: Structured logging with configurable levels
- **Metrics**: Trading performance and system health statistics
- **Notifications**: Telegram alerts for significant events
- **Web Dashboard**: Real-time monitoring interface

This architecture provides a robust, scalable foundation for automated trading while maintaining clear separation of concerns and extensibility for future enhancements.