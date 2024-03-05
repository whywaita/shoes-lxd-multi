FROM golang:1.22 AS builder

WORKDIR /go/src/github.com/whywaita/shoes-lxd-multi/server

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

COPY . .
RUN go build .

FROM alpine

RUN apk update \
  && apk update
RUN apk add --no-cache ca-certificates \
  && update-ca-certificates 2>/dev/null || true

COPY --from=builder /go/src/github.com/whywaita/shoes-lxd-multi/server/server /app

CMD ["/app"]
