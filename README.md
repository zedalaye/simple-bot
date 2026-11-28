# The Simple Trading Bot

This is a rewrite from [The Cryptomancien Bot Spot](https://github.com/cryptomancien/bot-spot) using [CCXT](https://github.com/ccxt/ccxt)

The original bot was tied to the MEXC exchange and the BTC/USDC trading pair.
This rewrite goes through CCXT and is able to trade any spot pair made available by the exchange.

The bot currently runs on [OKX](https://my.okx.com) (European, MiCA-licensed entity), spot only.
MEXC and Hyperliquid were supported in the past and have been removed.

Trading fees are taken into account: sell targets are raised by the maker round trip
(2 × maker fee) so that `profit_target` stays a *net* margin, orders are posted as
post-only (maker), and the real fees of every fill are recorded.

This bot also uses a SQLite 3 database to store positions and track orders.

## ⚠️ Important Disclaimer

### 🚨 **Not Investment Advice**
**This software is for educational and research purposes only. It does not constitute investment advice, financial advice, trading
advice, or any other type of advice. You should not construe any information provided here as a recommendation to buy, sell, or
hold any security, cryptocurrency, or other asset.**

### 💰 **Financial Risk**
**Trading cryptocurrencies involves substantial risk of loss. You may lose some or all of your invested capital. Past performance
does not guarantee future results. Cryptocurrency markets are highly volatile and can experience extreme price movements.**

### 🎓 **Required Knowledge & Experience**

#### **Trading & Financial Knowledge**
- Understanding of cryptocurrency markets and trading principles
- Knowledge of technical analysis and trading strategies
- Risk management and position sizing concepts
- Experience with algorithmic trading systems
- Understanding of exchange fees, slippage, and market mechanics

#### **Technical Knowledge**
- Programming and software development experience
- System administration and server management
- Network security and API authentication
- Database management and backup procedures
- Docker containerization and orchestration

#### **Production Deployment Experience**
- Running applications in production environments
- Monitoring system health and performance
- Implementing security best practices
- Backup and disaster recovery procedures
- Incident response and troubleshooting

### 👶 **Not Suitable for Beginners**
**This software is NOT intended for beginners or individuals without significant experience in both cryptocurrency trading and
software development. If you are new to trading, programming, or system administration, you should NOT use this software.**

**Before using this bot:**
- Paper trade with simulated money first
- Backtest strategies extensively
- Start with very small amounts
- Monitor performance continuously
- Have a clear exit strategy

### 🛡️ **Your Responsibility**
**By using this software, you acknowledge that:**
- You understand and accept all risks involved
- You have the necessary knowledge and experience
- You are solely responsible for your trading decisions
- You will comply with all applicable laws and regulations
- You have adequate insurance/capital reserves

### 🔒 **Security Notice**
**Cryptocurrency trading involves sensitive financial information. Ensure you:**
- Use strong, unique passwords and API keys
- Secure your private keys and wallet access
- Regularly update and patch your systems
- Monitor for security vulnerabilities
- Use reputable exchanges and services

### 📞 **No Support Guarantee**
**This is an open-source project provided "as-is" without any warranty or support guarantee. While the community may provide help,
there is no obligation to do so.**

---

*If you do not agree with these terms or lack the required knowledge and experience, please do not use this software.*

## The all-in-one script that uses Docker

Make sure [docker](https://www.docker.com/get-started/) is installed with the `docker compose` plugin

For each exchange you want to use, create a directory structure within the `storage` folder:

```
/storage/okx    # For the OKX Exchange
  /db/bot.db    # Will be created if it does not exist
  .env          # All config + credentials (copy from .env.example)

/storage/
  .env          # Shared config (CUSTOMER_ID, BOT_RELOAD_TOKEN, ...)
  .env.tg       # Telegram notifications (shared across instances)
```

Copy [`.env.example`](.env.example) to `storage/<exchange>/.env` and fill in the values.

```bash
$ ./simple-bot [*okx*|all] up
```

The syntax is :

```
$ ./simple-bot -h
Usage: ./simple-bot [<exchange>|all] [up|down|restart|logs|status|backup|run]
Examples:
  ./simple-bot okx up       # Lance bot + webui OKX
  ./simple-bot all up       # Lance toutes les instances de storage/
  ./simple-bot all down     # Arrête tout
  ./simple-bot all status   # Status de tous les containers
  ./simple-bot all backup   # Backup des bases de données
  ./simple-bot okx logs     # Logs OKX
  ./simple-bot okx run      # Lance une commande OKX
```

## Build the bot manually

Install [Go Lang](https://go.dev/learn/) 1.27.4, or use [Mise-en-Place](https://mise.jdx.dev/) : `mise install`
installs the exact versions pinned in `mise.toml` (Go and the CI analysis tools).

```bash
$ make release
```

The bot can also been built using docker.

```bash
$ make build-image
```

(or `make build-image`)

The executable is built into `/bin`

Everything ships as a **single binary**, `/bin/simple-bot`, that bundles every command as a
subcommand (`bot`, `web`, `admin`, `backtest`, `patternscan`, ...). `--root` is a global flag
handled before the command name:

```
simple-bot [--root DIR] <command> [command options...]
```

## Run the bot

### Common Configuration

All configuration is done through environment variables. Copy [`.env.example`](.env.example) to
`storage/<exchange>/.env` and fill in the values.

You will need a reload token to enable bot configuration updates from the Web UI:

```bash
$ ruby -r securerandom -e 'puts "BOT_RELOAD_TOKEN=#{SecureRandom.hex(20)}"'
BOT_RELOAD_TOKEN=888067c25c6d1f97a48f5e8e4820546e9a449a1a
```

Put it in `storage/.env` to share it across instances, or in each instance's `.env`.

### For OKX

Create an API key on [my.okx.com](https://my.okx.com) (European accounts: the API is
served by `eea.okx.com`) with the **Trade** permission only (no withdrawal), ideally
restricted to the server's IP.

Create `storage/okx/.env` (from `.env.example`) with at minimum:

```env
EXCHANGE=okx
TRADING_PAIR=BTC/USDC          # No USDT in the EU (MiCA): use USDC or EUR pairs
OKX_API_KEY=<OKX API Key for this bot>
OKX_SECRET=<OKX API Key "Secret">
OKX_PASSPHRASE=<Passphrase chosen when creating the key>
# OKX_DEMO=true                # Demo trading (fake money, needs demo API keys)
# MAKER_FEE_PCT=0.1            # Force the maker fee (default: read from the account)
```

Deposits land in the OKX *Funding* account: transfer the quote currency (USDC) to the
*Trading* account, which is the one the bot uses.

```bash
$ ./bin/simple-bot --root storage/okx bot
```

## Receive notifications on Telegram

Follow [this guide](https://dev.to/climentea/push-notifications-from-server-with-telegram-bot-api-32b3) to create a `storage/.env.tg` file :

*TL;DR*

* Run Telegram
* Search for `@BotFather` (beware of fake/spam homonyms! official link should be https://t.me/BotFather)
* Create a new bot using `/newbot`
* Answer questions
* In the final message gab the `Bot Token`, the token looks like `0123456789:Aa1Bb2Cc3-Aa1Bb2Cc3Dd4Ee5Ff6G-Aa1Bb2C`
* Use `curl` to find your new bot `Chat ID`

```bash
$ curl -X GET 'https://api.telegram.org/botTOKEN:FROM-BOTFATHER/getUpdates`
```
Your Chat ID is at `result.chat.id`

* Then create the `.env.tg` file

```env
TELEGRAM=1
TELEGRAM_BOT_TOKEN=<Bot token provided by Bot Father>
TELEGRAM_CHAT_ID=<Chat ID>
```

When the `storage/.env.tg` file is available, Telegram notifications are automatically enabled.

## Start the Web UI

```bash
$ ./bin/simple-bot --root storage/<exchange> web
```

## Backtesting strategies

The `backtest` command replays an `rsi_dca` strategy over the historical candles
stored in an instance database. It reuses the **real** decision code
(`internal/algorithms`) and the **real** indicator math (`internal/market`), so
results stay faithful to production behaviour. Decisions are taken at candle
close and orders only fill on later candles (no look-ahead bias). The database is
resolved from `--root` (the instance's `DB_PATH`).

```bash
# Single run reproducing an existing strategy (#1) on 15m price path
$ ./bin/simple-bot --root storage/okx backtest --strategy-id 1

# Parameter sweep: RSI timeframe × threshold × profit target × buy interval
$ ./bin/simple-bot --root storage/okx backtest \
    --rsi-tf 15m,1h --rsi-threshold 40,45,50 --profit 1,2 \
    --interval 21600,43200,86400 --vol-adj 0
```

Output columns: filled buys/day, closed cycles/day, median cycle duration,
peak simultaneous cycles, peak deployed capital, unsold inventory, realized
net P&L, return %, win rate. Use `--from`/`--to` (YYYY-MM-DD) to restrict the
period and `--fee` to set the per-side fee (default 0.1%). Note: `rsi_dca`
only sells at a profit target, so the win rate is ~100% by construction — the
real risk to watch is **inventory accumulation** (unsold cycles) during
downtrends.

## 📚 Documentation

Comprehensive documentation is available in the [`doc/`](doc/) directory:

- **[Quick Start](doc/README.md)** - Documentation hub and navigation
- **[System Architecture](doc/ARCHITECTURE.md)** - Component design and data flow
- **[Database Schema](doc/DATABASE.md)** - Tables, migrations, and relationships
- **[Available Binaries](doc/BINARIES.md)** - Executables and their purposes
- **[Web API Reference](doc/API.md)** - REST API endpoints and usage
- **[Development Guide](doc/DEVELOPMENT.md)** - Setup, testing, and contributing

### Key Topics

**For Users:**
- [Available Binaries](doc/BINARIES.md) - Understanding the different executables
- [Web API Reference](doc/API.md) - Using the REST API and web interface

**For Developers:**
- [System Architecture](doc/ARCHITECTURE.md) - Understanding the codebase structure
- [Database Schema](doc/DATABASE.md) - Data model and migrations
- [Development Guide](doc/DEVELOPMENT.md) - Contributing and testing
