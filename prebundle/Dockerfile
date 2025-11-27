FROM golang:1.25.4-alpine

ENV GO111MODULE=on \
  CGO_ENABLED=0 \
  GOPATH=/go

WORKDIR /app/tmp

RUN go mod init distcode

# 2. Warm up the cache
COPY warmup/modules.go .
RUN go mod tidy && go mod download && rm modules.go


COPY wrapper.go .
RUN go build -o wrapper ./wrapper.go && rm wrapper.go

CMD ["./wrapper", "cmd", "./prog"]
