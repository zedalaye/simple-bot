# Makefile pour simplifier la compilation

.PHONY: build-all \
        build-bot build-admin build-test build-volatility build-rsi build-order build-fix-cycles \
        build-image \
        clean \
        run-bot run-admin run-web run-test

# Construire tous les binaires
build-all: build-bot build-admin build-web build-test build-volatility build-rsi build-order build-fix-cycles

# Construire chaque binaire individuellement
build-bot:
	go build -o bin/bot ./cmd/bot

build-admin:
	go build -o bin/admin ./cmd/admin

build-web:
	go build -o bin/web ./cmd/web

build-test:
	go build -o bin/test ./cmd/test

build-volatility:
	go build -o bin/volatility ./cmd/volatility

build-rsi:
	go build -o bin/rsi ./cmd/rsi

build-order:
	go build -o bin/order ./cmd/order	

build-fix-cycles:
	go build -o bin/fix-cycles ./cmd/fix-cycles

# Construction de l'image docker
build-image:
	docker build -t zedalaye/simple-bot .

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

# Tests
# test:
# 	go test ./...

# Installation des dépendances
deps:
	go mod download
	go mod tidy