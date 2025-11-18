FROM golang:1.25-alpine AS builder
WORKDIR /go/src/github.com/distcodep7/dsnet
COPY . .
RUN go mod tidy && go mod vendor

# Build controller binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /controller ./main.go

# Base image for importing DSNet as a library (existing functionality)
FROM golang:1.25-alpine as base
WORKDIR /app
COPY --from=builder /go/src/github.com/distcodep7/dsnet/ ./
CMD ["sleep", "infinity"]

# Controller image for running the DSNet controller server
FROM alpine:latest AS controller
WORKDIR /app
COPY --from=builder /controller .
CMD ["./controller"]