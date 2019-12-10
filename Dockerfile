FROM golang:alpine
LABEL maintainer "Ilya Glotov <ilya@ilyaglotov.com>"

ENV GO111MODULE=on \
    CGO_ENABLED=0

COPY . /go/pbmon

WORKDIR /go/pbmon/examples/logger

RUN go build -ldflags="-s -w" -a -installsuffix static -o /pbmon

FROM alpine:edge

COPY --from=0 /pbmon /pbmon

RUN apk add --no-cache ca-certificates

ENTRYPOINT ["/pbmon"]
