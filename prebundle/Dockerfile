FROM golang:1.25.4-alpine

ENV GO111MODULE=on \
  CGO_ENABLED=0 \
  GOPATH=/go

WORKDIR /app/tmp

COPY warmup/modules.go .
COPY wrapper.go .

RUN apk add --no-cache curl ca-certificates && \
  go mod init distcode && \
  go mod tidy && go mod download && \
  rm modules.go && \
  go build -o wrapper ./wrapper.go && \
  rm wrapper.go && \
  chmod +x wrapper


CMD ["./wrapper", "cmd", "./prog"]
