# The Simple Trading Bot

This is a rewrite from [The Cryptomancien Bot Spot](https://github.com/cryptomancien/bot-spot) using [CCXT](https://github.com/ccxt/ccxt)

The original bot was tied to the MEXC exchange and the BTC/USDC trading pair.
This rewrite enables the bot to use all exchanges available through CCXT and is able to trade all pairs made available by the exchange.

For now, MEXC anf Hyperliquid have been tested.

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
/storage/mexc   # For the MEXC Exchange
  /db/bot.db    # Will be created if it does not exist
  config.yml
  .env          # Define API_KEY and SECRET

/storage/hl     # For the Hyperliquid Exchange
  /db/bot.db    # Will be created if it does not exist
  config.yml
  .env          # Define WALLET_ADDRESS and PRIVATE_KEY

/storage/
  .env          # Define CUSTOMER_ID and secrets common to all instances
  .env.tg       # To send notifications through a Telgram Bot Instance  
```

Create or adapt the [`config.yml`](storage/mexc/config.yml) and `.env` files (for exchange secrets), see below.

Make sure `config.yml` specifies a database name like `db/bot.db`

```bash
$ ./simple-bot [*mexc*|hl|all] up
```

The syntax is :

```
$ ./simple-bot -h
Usage: ./simple-bot [<exchange>|all] [up|down|restart|logs|status|backup|run]
Examples:
  ./simple-bot mexc up      # Lance bot + webui MEXC
  ./simple-bot hl up        # Lance bot + webui Hyperliquid
  ./simple-bot all up       # Lance MEXC ET Hyperliquid
  ./simple-bot all down     # Arrête tout
  ./simple-bot all status   # Status de tous les containers
  ./simple-bot all backup   # Backup des bases de données
  ./simple-bot mexc logs    # Logs MEXC
  ./simple-bot mexc run     # Lance une commande MEXC
```

## Build the bot manually

Install latest [Go Lang](https://go.dev/learn/) or use [Mise-en-Place](https://mise.jdx.dev/)

```bash
$ make release
```

The bot can also been built using docker.

```bash
$ make build-image
```

(or `make build-image`)

Executables are built into `/bin`

The main executable is `/bin/bot`

## Run the bot

### Common Configuration

You will need a Reload Token to enabled bot configuration updates from Web UI.
Create a `storage/.env` file and fill the `BOT_RELOAD_TOKEN` with random values :

```env
BOT_RELOAD_TOKEN=888067c25c6d1f97a48f5e8e4820546e9a449a1a
```

Example using Ruby :

```irb
❯ ruby -r securerandom -e 'puts "BOT_RELOAD_TOKEN=#{SecureRandom.hex(20)}"'
BOT_RELOAD_TOKEN=888067c25c6d1f97a48f5e8e4820546e9a449a1a
```

### For MEXC

Adjust bot parameters in [`storage/mexc/config.yml`](storage/mexc/config.yml) and create a `storage/mexc/.env` file containing :

```env
API_KEY=<MEXC API Key for this bot>
SECRET=<MEXC API Key "Secret">
```

```bash
$ ./bin/bot --root storage/mexc
```

### For Hyperliquid

Adjust bot parameters in [`storage/hl/config.yml`](storage/hl/config.yml) and create a `storage/hl/.env` file containing secrets :

```env
WALLET_ADDRESS=<YOUR Wallet Address on Arbitrum Blockchain>
PRIVATE_KEY=<The Hyperliquid Private Key of an API Key you create for this bot>
```

```bash
$ ./bin/bot --root storage/hl
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
$ ./bin/web --root storage/<exchange>
```

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
