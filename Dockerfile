FROM alpine:latest AS build

RUN apk add --no-cache --update go gcc g++ make

RUN mkdir /go
ENV GOPATH=/go

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=1 GOOS=linux make

FROM alpine:latest

WORKDIR /app

COPY --from=build /app/templates/ /app/templates/
COPY --from=build /app/bin/* .

EXPOSE 8080/tcp
CMD ["./web"]