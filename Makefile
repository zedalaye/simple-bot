DOCKER_IMAGE ?= zedalaye/simple-bot
PLATFORMS ?= linux/amd64 #,linux/arm64

GIT_TAG := $(shell git describe --tags --always --dirty)
VERSION  ?= $(GIT_TAG)

.PHONY: build-all build-simple-bot \
        build-image push-image \
        clean \
        run-bot run-admin run-web run-test run-backtest \
        fmt vet check vulncheck \
        deps deps-check deps-update deps-verify

all: build-all

# Use Target Specifioed Variables to "build-all" in release mode (extra -ldflags added to children go build commands)
release: FLAGS = -ldflags "-s -w -X bot/internal/version.Version=$(VERSION)"
release: build-all

# Toutes les commandes (bot, web, admin, backtest, patternscan, order, rsi, volatility,
# test) sont regroupées dans un binaire unique : le code commun n'est compilé qu'une fois.
build-all: build-simple-bot

build-simple-bot:
	go build -o bin/simple-bot ${FLAGS} ./cmd/simple-bot

# Construction de l'image docker (précédée des vérifications dépendances + vulnérabilités)
# deps-check est informatif (liste les MAJ dispo, ne bloque pas) ; deps-verify et vulncheck sont bloquants
build-image: deps-check deps-verify vulncheck
	docker build --pull --platform ${PLATFORMS} \
		--build-arg VERSION=$(VERSION) \
		-t ${DOCKER_IMAGE}:$(VERSION) \
		-t ${DOCKER_IMAGE}:latest \
		.

push-image:
	docker push ${DOCKER_IMAGE}:$(VERSION)
	docker push ${DOCKER_IMAGE}:latest

# Nettoyer les binaires
clean:
	rm -rf bin/

# Lancer les services (--root est un flag GLOBAL, il précède le nom de la sous-commande).
# Ex. make run-bot ARGS="--root storage/mexc bot --buy-at-launch"
run-bot:
	go run ./cmd/simple-bot $(ARGS) bot

run-admin:
	go run ./cmd/simple-bot $(ARGS) admin

run-web:
	go run ./cmd/simple-bot $(ARGS) web

run-test:
	DEBUG=true go run ./cmd/simple-bot $(ARGS) test

# Backtest : ex. make run-backtest ARGS="--root storage/mexc" puis options : ARGS="--root storage/mexc" \
#   BTARGS="--rsi-tf 15m,1h --rsi-threshold 40,45,50 --profit 1,2 --interval 21600,43200,86400"
run-backtest:
	go run ./cmd/simple-bot $(ARGS) backtest $(BTARGS)

# Vérifications avant commit
fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet build-all

# Vérifier l'intégrité des modules téléchargés (go.sum) — bloquant
deps-verify:
	go mod verify

# Scanner les vulnérabilités connues du code et des dépendances — bloquant
# (sans installation globale : exécuté à la volée)
vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Installation des dépendances
deps:
	go mod download
	go mod tidy

# Vérifier les mises à jour disponibles des dépendances directes (sans modifier go.mod)
# Informatif : n'échoue jamais (|| true) pour ne pas bloquer build-image
deps-check:
	@echo "==> Dépendances directes avec mise à jour disponible :"
	@grep -E '^\s+\S+ v' go.mod | grep -v '// indirect' | awk '{print $$1}' | xargs go list -u -m -f '{{if .Update}}{{.Path}}: {{.Version}} → {{.Update.Version}}{{end}}' || true

# Mettre à jour toutes les dépendances vers leur dernière version + tidy
deps-update:
	go get -u ./...
	go mod tidy
