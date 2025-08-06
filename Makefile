# Makefile pour simplifier la compilation

.PHONY: build-all build-bot build-admin build-test clean run-bot run-admin run-test

# Construire tous les binaires
build-all: build-bot build-admin build-test

# Construire chaque binaire individuellement
build-bot:
	go build -o bin/bot ./cmd/bot

build-admin:
	go build -o bin/admin ./cmd/admin

build-test:
	go build -o bin/test ./cmd/test

# Nettoyer les binaires
clean:
	rm -rf bin/

# Lancer les services
run-bot:
	go run ./cmd/bot

run-admin:
	go run ./cmd/admin

run-test:
	DEBUG=true go run ./cmd/test

# Tests
# test:
# 	go test ./...

# Installation des dépendances
deps:
	go mod download
	go mod tidy