FROM alpine:edge AS build

RUN apk add --no-cache --update go gcc g++ make

RUN mkdir /go
ENV GOPATH=/go

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=1 GOOS=linux make

FROM alpine:edge

WORKDIR /app

COPY --from=build /app/templates/ /app/templates/
COPY --from=build /app/bin/* .

CMD ["./web"]