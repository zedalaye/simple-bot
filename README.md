# The Simple Trading Bot

This is a rewrite from [The Cryptomancien Bot Spot](https://github.com/cryptomancien/bot-spot) using [CCXT](https://github.com/ccxt/ccxt)

The original bot was tied to the MEXC exchange and the BTC/USDC trading pair.
This rewrite enables the bot to use all exchanges available through CCXT and is able to trade all pairs made available by the exchange.

For now, MEXC anf Hyperliquid have been tested.

This bot also uses a SQLite 3 database to store positions and track orders.

## The all-in-one script that uses Docker

Make sure [docker](https://www.docker.com/get-started/) is installed.

Create or adapt the `config.yml` and `.env` file (for exchange secrets), see below.

Make sure `config.yml` references the right `.env` and specifies a database name like `bot.db`

```bash
$ ./run.sh <config.yml> [bot]
```

The syntax is :

```
./run.sh [config.yml] [command]
Accepted commands are: bot (default), web, admin [subcommand] [format]
admin command requires a subcommand and accept a format parameter
 subcommands:   stats, orders, positions, export
 formats:       table, json
```

## Build the bot manually

Install latest [Go Lang](https://go.dev/learn/) or use [Mise-en-Place](https://mise.jdx.dev/)

```bash
$ make
```

The bot can also been built using docker.

```bash
$ docker build -t simple-bot .
```

Executables are built into `/bin`

The main executable is `/bin/bot`

## Run the bot

### For MEXC

Adjust bot parameters in `config-mexc.yml` and create a `.env.mexc` file containing :

```env
API_KEY=<MEXC API Key for this bot>
SECRET=<MEXC API Key "Secret">
```

```bash
$ ./bin/bot --config config-mexc.yml
```

### For Hyperliquid

Adjust bot parameters in `config-hl.yml` and create a `.env.hl` file containing secrets :

```env
WALLET_ADDRESS=<YOUR Wallet Address on Arbitrum Blockchain>
PRIVATE_KEY=<The Hyperliquid Private Key of an API Key you create for this bot>
```

```bash
$ ./bin/bot --config-hl.yml
```

## Receive notifications on Telegram

Follow [this guide](https://dev.to/climentea/push-notifications-from-server-with-telegram-bot-api-32b3) to create a `.env.tg` file :

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

When the `.env.tg` file is available, Telegram notifications are automatically enabled.

## Start the Web UI

```bash
$ ./bin/web --config <the config file.yml>
```

```bash
$ ./run.sh <config.yml> web
```