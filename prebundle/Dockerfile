FROM golang:1.25.4-alpine

ENV GO111MODULE=on \
  CGO_ENABLED=0 \
  GOPATH=/go

WORKDIR /app/tmp

RUN go mod init distcode

COPY warmup/modules.go .
RUN go mod tidy && go mod download && rm modules.go


COPY wait_signal.go .
RUN go build -o wait_signal wait_signal.go

CMD ["./wait_signal"]
