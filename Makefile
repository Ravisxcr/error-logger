.PHONY: all build run test vet fmt docker-build docker-up docker-down docker-logs test-sdks test-python test-node test-go clean help

BIN_DIR := bin
BINARY := $(BIN_DIR)/error-logger
PORT := 9000
DATA_DIR := data

all: build

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed -e 's/## /  /'

## build: Compile the error-logger server binary
build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BINARY) ./cmd/server
	@echo "Built binary at $(BINARY)"

## run: Run error-logger server locally
run:
	go run ./cmd/server -addr :$(PORT) -data-dir $(DATA_DIR)

## test: Run unit tests
test:
	go test -v ./...

## vet: Run go vet on all packages
vet:
	go vet ./...

## fmt: Format all Go source files
fmt:
	go fmt ./...

## docker-build: Build the error-logger Docker image
docker-build:
	docker build -t error-logger:latest .

## docker-up: Start error-logger in background with Docker Compose
docker-up:
	docker compose up -d --build

## docker-down: Stop and remove Docker Compose containers
docker-down:
	docker compose down

## docker-logs: View live logs from Docker Compose
docker-logs:
	docker compose logs -f

## test-sdks: Run all SDK test clients in isolated Docker Compose environment
test-sdks:
	docker compose -f docker-compose.test.yml up --build --abort-on-container-exit
	@docker compose -f docker-compose.test.yml down

## test-python: Run Python SDK demo client locally against running server
test-python:
	@if [ -d "demo/.venv" ]; then \
		. demo/.venv/bin/activate && python3 demo/main.py; \
	else \
		python3 demo/main.py; \
	fi

## test-node: Run Node.js SDK demo client locally against running server
test-node:
	cd demo/node && npm install --no-audit --no-fund && node index.js

## test-go: Run Go SDK demo client locally against running server
test-go:
	cd demo/go && go mod tidy && go run main.go

## clean: Remove build artifacts and temporary files
clean:
	rm -rf $(BIN_DIR) error-logger
	@echo "Cleaned build artifacts."

