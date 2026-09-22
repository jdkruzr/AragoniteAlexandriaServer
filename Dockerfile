# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/alexandria-server ./cmd/alexandria-server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates poppler-utils tzdata \
    && addgroup -S -g 10001 alexandria \
    && adduser -S -D -H -u 10001 -G alexandria alexandria
COPY --from=builder /out/alexandria-server /usr/local/bin/alexandria-server
USER 10001:10001
EXPOSE 8443 8089
ENTRYPOINT ["alexandria-server"]
