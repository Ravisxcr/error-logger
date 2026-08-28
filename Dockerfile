# Build stage
FROM --platform=$BUILDPLATFORM golang:alpine AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o error-logger ./cmd/server

# Run stage
FROM alpine:latest
WORKDIR /app/data
WORKDIR /app
COPY --from=builder /app/error-logger .
ENV PORT=9000 \
    DATA_DIR=/app/data

EXPOSE 9000
ENTRYPOINT ["./error-logger"]


