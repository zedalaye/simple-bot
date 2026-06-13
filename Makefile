DOCKER_IMAGE ?= zedalaye/simple-bot
PLATFORMS ?= linux/amd64 #,linux/arm64

GIT_TAG := $(shell git describe --tags --always --dirty)
VERSION  ?= $(GIT_TAG)

.PHONY: build-all \
        build-bot build-admin build-web build-test build-volatility build-rsi build-order \
        build-image push-image \
        clean \
        run-bot run-admin run-web run-test \
        fmt vet check \
        deps deps-check deps-update

all: build-all

# Use Target Specifioed Variables to "build-all" in release mode (extra -ldflags added to children go build commands)
release: FLAGS = -ldflags "-s -w -X bot/internal/version.Version=$(VERSION)"
release: build-all

# Construire tous les binaires
build-all: build-bot build-web build-admin build-test build-volatility build-rsi build-order

# Construire chaque binaire individuellement
build-bot:
	go build -o bin/bot ${FLAGS} ./cmd/bot

build-admin:
	go build -o bin/admin ${FLAGS} ./cmd/admin

build-web:
	go build -o bin/web ${FLAGS} ./cmd/web

build-test:
	go build -o bin/test ./cmd/test

build-volatility:
	go build -o bin/volatility ./cmd/volatility

build-rsi:
	go build -o bin/rsi ./cmd/rsi

build-order:
	go build -o bin/order ./cmd/order

# Construction de l'image docker
build-image:
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

# Lancer les services
run-bot:
	go run ./cmd/bot $(ARGS)

run-admin:
	go run ./cmd/admin $(ARGS)

run-web:
	go run ./cmd/web $(ARGS)

run-test:
	DEBUG=true go run ./cmd/test $(ARGS)

# Vérifications avant commit
fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet build-all

# Installation des dépendances
deps:
	go mod download
	go mod tidy

# Vérifier les mises à jour disponibles des dépendances directes (sans modifier go.mod)
deps-check:
	@grep -E '^\s+\S+ v' go.mod | grep -v '// indirect' | awk '{print $$1}' | xargs go list -u -m -f '{{if .Update}}{{.Path}}: {{.Version}} → {{.Update.Version}}{{end}}'

# Mettre à jour toutes les dépendances vers leur dernière version + tidy
deps-update:
	go get -u ./...
	go mod tidy
