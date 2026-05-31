# Simple Trading Bot - Documentation

Welcome to the Simple Trading Bot documentation! This comprehensive guide covers everything you need to understand, deploy, and extend the trading bot.

## Disclaimer

This set of documentation has been generated automatically by an AI. Please report any inconsistencies.

## 🚀 Quick Start

### Prerequisites
- [Go 1.21+](https://go.dev/dl/) or [Mise-en-Place](https://mise.jdx.dev/)
- [Docker & Docker Compose](https://docs.docker.com/get-docker/) (for containerized deployment)

### Basic Setup
```bash
# Clone and build
git clone <repository-url>
cd simple-trading-bot
make

# Update configuration 
# -> storage/mexc/.env (copy from .env.example)
# -> storage/hl/.env (copy from .env.example)
# -> storage/.env
# -> storage/.env.tg

# Run with Docker (recommended)
./simple-bot mexc up    # For MEXC exchange
./simple-bot hl up      # For Hyperliquid exchange
```

### Configuration
Create exchange-specific directories under `storage/` (copy `.env.example` as a starting point):
```
storage/mexc/.env          # MEXC config + API keys
storage/hl/.env            # Hyperliquid config + wallet credentials
storage/.env               # Common secrets (BOT_RELOAD_TOKEN, CUSTOMER_ID)
storage/.env.tg            # Common Telegram notifications using BotFather instance
```

## 📚 Documentation Index

### 🏗️ **Architecture & Design**
- **[ARCHITECTURE.md](ARCHITECTURE.md)** - System design, components, and data flow
- **[DATABASE.md](DATABASE.md)** - Database schema, migrations, and data model

### 🛠️ **Usage & Operation**
- **[BINARIES.md](BINARIES.md)** - Available executables and their purposes
- **[API.md](API.md)** - Web API endpoints and usage

### 💻 **Development**
- **[DEVELOPMENT.md](DEVELOPMENT.md)** - Development setup, testing, and contributing

## 🎯 Key Features

- **Multi-Exchange Support**: MEXC and Hyperliquid exchanges
- **Strategy-Based Trading**: Cron-scheduled strategies with algorithm registry
- **Decoupled Buy/Sell Logic**: Independent execution timing for buy and sell operations
- **Market Data Caching**: Efficient candle storage with indicator calculations
- **Web Interface**: REST API and web UI for monitoring and management
- **SQLite Database**: Robust data persistence with incremental migrations
- **Telegram Notifications**: Real-time trading alerts
- **Docker Integration**: Containerized deployment with multi-exchange support

## 🔧 Core Concepts

### Trading Strategies
The bot supports multiple trading strategies that run on configurable schedules:
- **Buy Strategies**: Execute based on cron expressions (e.g., every 5 minutes)
- **Sell Strategies**: Check every 5 minutes for profit-taking opportunities
- **Algorithm Registry**: Pluggable trading algorithms (RSI, MACD, volatility-based)

### Order Lifecycle
1. **Strategy Evaluation** → 2. **Signal Generation** → 3. **Order Placement** → 4. **Execution Monitoring** → 5. **Profit Realization**

### Market Data
- Automatic candle collection and caching
- Built-in technical indicators (RSI, MACD, moving averages)
- Precision-aware calculations for different trading pairs

## 📖 Learning Path

**New Users:**
1. Read [BINARIES.md](BINARIES.md) to understand available tools
2. Follow the main README.md setup instructions
3. Explore [API.md](API.md) for web interface usage

**Developers:**
1. Study [ARCHITECTURE.md](ARCHITECTURE.md) for system understanding
2. Review [DATABASE.md](DATABASE.md) for data model details
3. Follow [DEVELOPMENT.md](DEVELOPMENT.md) for contribution guidelines

**Power Users:**
1. All documentation for comprehensive understanding
2. Check archived docs in `doc/archive/` for historical context

## 🤝 Contributing

We welcome contributions! Please see [DEVELOPMENT.md](DEVELOPMENT.md) for detailed contribution guidelines.

## 📄 License

This project is licensed under the MIT License - see the main README.md for details.

---

*For questions or support, please check the main project README.md or create an issue in the repository.*