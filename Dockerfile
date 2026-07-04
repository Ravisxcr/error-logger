# Build stage
FROM golang:alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o error-logger ./cmd/server

# Run stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/error-logger .
RUN mkdir -p /app/data
EXPOSE 9000
CMD ["./error-logger", "-addr", ":9000", "-data-dir", "/app/data"]
