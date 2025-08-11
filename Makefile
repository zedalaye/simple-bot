# Makefile pour simplifier la compilation

.PHONY: build-all build-bot build-admin build-test build-volatility clean run-bot run-admin run-test run-volatility

# Construire tous les binaires
build-all: build-bot build-admin build-test build-web build-volatility

# Construire chaque binaire individuellement
build-bot:
	go build -o bin/bot ./cmd/bot

build-admin:
	go build -o bin/admin ./cmd/admin

build-test:
	go build -o bin/test ./cmd/test

build-volatility:
	go build -o bin/volatility ./cmd/volatility

build-web:
	go build -o bin/web ./cmd/web

# Nettoyer les binaires
clean:
	rm -rf bin/

# Lancer les services
run-bot:
	go run ./cmd/bot

run-admin:
	go run ./cmd/admin

run-web:
	go run ./cmd/web

run-test:
	DEBUG=true go run ./cmd/test

run-volatility:
	DEBUG=true go run ./cmd/volatility

# Tests
# test:
# 	go test ./...

# Installation des dépendances
deps:
	go mod download
	go mod tidy