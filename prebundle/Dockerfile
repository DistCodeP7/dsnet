FROM golang:1.25.4-alpine

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOPATH=/go

WORKDIR /app/tmp

# Copy source and module files
COPY warmup/modules.go .
COPY wrapper.go .
COPY go.mod .
COPY go.sum .
COPY ./prog ./prog

# Install required packages and build the wrapper binary
RUN apk add --no-cache curl ca-certificates bash git \
    && go mod tidy \
    && go mod download \
    && rm modules.go \
    && go build -o wrapper ./wrapper.go \
    && rm wrapper.go \
    && chmod +x wrapper

# Command to run
CMD ["./wrapper", "cmd", "./prog"]