FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files and source code first
COPY go.mod main.go ./

# Download dependencies
RUN go mod download
RUN go get github.com/gorilla/handlers
RUN go get github.com/gorilla/mux

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/main .

EXPOSE 8080

CMD ["./main"]
