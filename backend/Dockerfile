# Build stage
FROM golang:1.27-alpine AS builder

WORKDIR /app

# Unduh dependensi dulu (cache layer)
COPY go.mod go.sum ./
RUN go mod download

# Salin source dan build binary statis
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/main .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates \
    && adduser -D -H -u 10001 app

WORKDIR /app
COPY --from=builder /out/main /app/main

USER app
EXPOSE 8080

# /health/ready = proses hidup DAN database terhubung
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/health/ready || exit 1

CMD ["/app/main"]
