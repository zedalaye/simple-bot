# The Simple Trading Bot

This is a rewrite from [The Cryptomancien Bot Spot](https://github.com/cryptomancien/bot-spot) using [CCXT](https://github.com/ccxt/ccxt)

The original bot was tied to the MEXC exchange and the BTC/USDC trading pair.
This rewrite enables the bot to use all exchanges available through CCXT and is able to trade all pairs made available by the exchange.

For now, MEXC anf Hyperliquid have been tested.

This bot also uses a SQLite 3 database to store positions and track orders.

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
$ make
```

The bot can also been built using docker.

```bash
$ docker build -t zedalaye/simple-bot .
```

(or `make build-image`)

Executables are built into `/bin`

The main executable is `/bin/bot`

## Run the bot

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
