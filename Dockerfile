FROM golang:1.25-alpine AS builder
WORKDIR /go/src/github.com/distcode/dsnet
COPY . .
RUN go mod tidy && go mod vendor

FROM golang:1.25-alpine
WORKDIR /app

# Copy module root with vendored deps
COPY --from=builder /go/src/github.com/distcode/dsnet/ ./

CMD ["sleep", "infinity"]
