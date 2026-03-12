FROM golang:1.24 AS builder

WORKDIR /go/src/github.com/whywaita/shoes-lxd-multi/scheduler

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

COPY . .
RUN go build -o /go/src/github.com/whywaita/shoes-lxd-multi/scheduler/ .

FROM alpine

RUN apk update
RUN apk add --no-cache ca-certificates \
  && update-ca-certificates 2>/dev/null || true

COPY --from=builder /go/src/github.com/whywaita/shoes-lxd-multi/scheduler/scheduler /app

CMD ["/app"]
