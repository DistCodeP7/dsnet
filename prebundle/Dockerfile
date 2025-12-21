# --- Build stage ---
FROM golang:1.25.4-alpine AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOPATH=/go \
    GOCACHE=/go/cache

WORKDIR /app/tmp

COPY go.mod go.sum ./
COPY . .

RUN apk add --no-cache curl ca-certificates bash git \
    && go mod tidy \
    && go mod download \
    && go build -o /app/wrapper ./wrapper.go

# --- Runtime stage ---
FROM golang:1.25.4-alpine AS runtime

ENV GOPATH=/go \
    GOCACHE=/go/cache

WORKDIR /app

RUN apk add --no-cache iputils coreutils bash git

COPY --from=builder /app/wrapper .

RUN mkdir -p /app/tmp

EXPOSE 8090

CMD ["./wrapper", "cmd", "./prog"]