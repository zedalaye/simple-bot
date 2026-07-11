# syntax=docker/dockerfile:1
# https://stackoverflow.com/a/76440207
# https://github.com/tonistiigi/xx

## Provide cross-platform compilation helpers
FROM --platform=$BUILDPLATFORM tonistiigi/xx AS xx

## Builder environment
FROM --platform=$BUILDPLATFORM golang:alpine AS build

# Install clang and co. for BUILDPLATFORM
RUN apk add --no-cache --update clang lld make

# Copy helpers
COPY --from=xx / /

# CGO_ENABLED=1 requires GCC for TARGETPLATFORM... and we need CGO for SQLite3
ARG TARGETPLATFORM
RUN xx-apk add musl-dev gcc

WORKDIR /app
COPY go.mod go.sum ./

ENV CGO_ENABLED=1
# Cache mount du module cache : évite de re-télécharger les dépendances à chaque build
RUN --mount=type=cache,target=/go/pkg/mod \
    xx-go mod download

COPY . ./

ARG VERSION=dev

# Wrap xx-go into go so that we can use our Makefile with no changes
RUN xx-go --wrap
# Cache mounts du module cache ET du cache de compilation Go : ccxt (énorme) n'est
# compilé en entier qu'une fois, les builds suivants ne recompilent que le code modifié.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make release VERSION=${VERSION}

## Run environment
FROM alpine:latest

# To check the architecture of built binaries
RUN apk add --no-cache --update file tzdata
ENV TZ=Europe/Paris

WORKDIR /app
COPY --from=build /app/templates/ /app/templates/
COPY --from=build /app/bin/simple-bot .

# simple-bot regroupe toutes les commandes ; « web » sert la WebUI sur le port 8080.
EXPOSE 8080/tcp
CMD ["./simple-bot", "admin"]
