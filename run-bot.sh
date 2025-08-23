#!/bin/bash

# Will load and parse everything from config.yml to map the database and feed expected environment3

# Build the container using
# $ docker build -t simple-bot .
IMAGE=simple-bot:latest

if [ -z `docker images -q ${IMAGE} 2> /dev/null` ]; then
  docker build -t ${IMAGE} .
fi

CONFIG=${1:-config.yml}

if [ ! -f "${CONFIG}" ]; then
  echo "${CONFIG} does not exist"
  exit 1
fi

# Extract base .env file name from config file
ENV_BASE=$(grep -Po "env: \K(\.env[^ ]+)" ${CONFIG})

# Extract database file name from config file
DB=$(grep -A1 "database:" ${CONFIG} | grep -Po "path: \K(.*\.db)")

# Build the `docker run` command line
ARGS=(-v ./${CONFIG}:/app/config.yml)
if [ "$DB" != "" ]; then
  ARGS+=(-v ./${DB}:/app/${DB})
fi
if [ "$ENV_BASE" != "" ]; then
  ARGS+=(--env-file ${ENV_BASE})
fi
if [ -f .env.tg ]; then
  ARGS+=(--env-file .env.tg)
fi

docker run ${ARGS[@]} ${IMAGE} /app/bot
