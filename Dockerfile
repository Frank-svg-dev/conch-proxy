# syntax=docker/dockerfile:1

FROM docker.m.daocloud.io/library/golang:1.25-alpine AS builder
WORKDIR /app

RUN apk add --no-cache ca-certificates

ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/conch-proxy ./cmd/server

FROM docker.m.daocloud.io/library/alpine:3.22
WORKDIR /app

RUN apk add --no-cache ca-certificates && adduser -D -u 10001 appuser

COPY --from=builder /out/conch-proxy /app/conch-proxy
COPY server.crt server.key /app/

USER appuser
EXPOSE 8080

ENTRYPOINT ["/app/conch-proxy"]
