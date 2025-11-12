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
RUN xx-go mod download

COPY . ./

# Wrap xx-go into go so that we can use our Makefile with no changes
RUN xx-go --wrap
RUN make release

## Run environment
FROM alpine:latest

# To check the architecture of built binaries
RUN apk add --no-cache --update file tzdata
ENV TZ=Europe/Paris

WORKDIR /app
COPY --from=build /app/templates/ /app/templates/
COPY --from=build /app/bin/* .

# bin/web provides a WebUI and starts by default on port 8080
EXPOSE 8080/tcp
CMD ["./admin"]
