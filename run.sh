#!/bin/bash

# Will load and parse everything from config.yml to map the database and feed expected environment3

IMAGE=simple-bot:latest

# Build the image if it does not exist
if [ -z "$(docker images -q ${IMAGE} 2> /dev/null)" ]; then
  docker build -t ${IMAGE} .
fi

CONFIG=${1:-config.yml}
COMMAND=${2:-bot}

if [ ! -f "${CONFIG}" ]; then
  echo "${CONFIG} configuration file does not exist"

  echo "Syntax: ./run.sh [config file] [command]"
  echo "Accepted commands are: bot, web, admin [subcommand] [format]"
  echo "admin command requires a subcommand and accept a format parameter"
  echo " subcommands:   stats, orders, positions, export"
  echo " formats:       table, json"

  exit 1
fi

# Extract base .env file name from config file
ENV_BASE=$(grep -Po "env: \K(\.env[^ ]+)" "${CONFIG}")

# Extract database file name from config file
DB=$(grep -A1 "database:" "${CONFIG}" | grep -Po "path: \K(.*\.db)")

# Build the `docker run` command line
ARGS=(-v "./${CONFIG}:/app/config.yml")
if [ "$DB" != "" ]; then
  ARGS+=(-v "./${DB}:/app/${DB}")
fi
if [ "$ENV_BASE" != "" ]; then
  ARGS+=(--env-file "${ENV_BASE}")
fi
if [ -f .env.tg ]; then
  ARGS+=(--env-file .env.tg)
fi

case $COMMAND in
bot)
  ;;

web)
  # Forward the port
  ARGS+=(-p8080:8080)
  ;;

admin)
  # Parse and format extra parameters for admin command
  ADMIN_SUBCOMMAND=${3:?"For admin command you must supply a subcommand"}

  case ${ADMIN_SUBCOMMAND} in
  stats|positions|orders|export)
    ADMIN_SUBCOMMAND="--cmd ${ADMIN_SUBCOMMAND}"
    ;;
  *)
    echo "${ADMIN_SUBCOMMAND} is not supported (supported: stats, positions, orders, exports)"
    exit 1
    ;;
  esac

  ADMIN_FORMAT=$4

  if [ "${ADMIN_FORMAT}" != "" ]; then
    case ${ADMIN_FORMAT} in
    json|table)
      ADMIN_FORMAT="--format ${ADMIN_FORMAT}"
      ;;
    *)
      echo "${ADMIN_FORMAT} is not a valid format for admin command (accepted: json or table)"
      exit 1
      ;;
    esac
  fi
  ;;

*)
  echo "$COMMAND is not a valid bot command"
  exit 1
  ;;
esac

docker run "${ARGS[@]}" ${IMAGE} "/app/${COMMAND}" ${ADMIN_SUBCOMMAND} ${ADMIN_FORMAT}
