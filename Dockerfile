FROM golang:1.26.3-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/identity-service ./cmd

FROM alpine:3.23

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 identity \
    && adduser -S -D -H -u 10001 -G identity identity
WORKDIR /app
COPY --from=build /out/identity-service /app/identity-service
COPY migrations/ /app/migrations/

ENV GIN_MODE=release PORT=8080 GRPC_PORT=9090
USER identity:identity
EXPOSE 8080 9090
HEALTHCHECK --interval=10s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${PORT}/health" || exit 1
ENTRYPOINT ["/app/identity-service"]
