# Build stage
FROM --platform=$BUILDPLATFORM golang:1.24-alpine3.21 AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# Cache Go modules layer (invalidated only when go.mod or go.sum changes)
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy application source code
COPY . .

# Compile binary using BuildKit Go compiler cache & module cache mounts for near-instant incremental builds
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /app/error-logger ./cmd/server

# Run stage: 'scratch' has zero dependencies, NO OpenSSL, and instant build/startup
FROM scratch

# Root CA certificates (already present in the golang builder image)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /app

# Copy the statically compiled binary
COPY --from=builder /app/error-logger /app/error-logger

ENV PORT=9000 \
    DATA_DIR=/app/data

EXPOSE 9000

ENTRYPOINT ["/app/error-logger"]
