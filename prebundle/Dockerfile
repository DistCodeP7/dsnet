# --- Build stage ---
FROM golang:1.25.4-alpine AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOPATH=/go

WORKDIR /app/tmp

COPY go.mod go.sum ./
COPY . .

# Install dependencies and build wrapper binary
RUN apk add --no-cache curl ca-certificates \
    && go mod tidy \
    && go mod download \
    && go build -o /app/wrapper ./wrapper.go

# --- Runtime stage ---
FROM alpine:latest

WORKDIR /app

# Install runtime utilities required by distcode_worker tests (ping, timeout)
RUN apk add --no-cache iputils coreutils

# Copy only the binary and preserve /tmp folder
COPY --from=builder /app/wrapper .
RUN mkdir -p /app/tmp

EXPOSE 8090

CMD ["./wrapper", "cmd", "./prog"]