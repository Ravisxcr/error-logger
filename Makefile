.PHONY: all build run test vet fmt docker-build docker-tag docker-push docker-release docker-up docker-down docker-logs test-sdks test-python test-node test-go clean help

BIN_DIR := bin
BINARY := $(BIN_DIR)/error-logger
PORT := 9000
DATA_DIR := data

VERSION := $(shell cat VERSION)
MAJOR := $(shell echo $(VERSION) | cut -d. -f1)
DOCKER_USER ?= ravisxcr
IMAGE_NAME ?= error-logger
IMAGE := $(DOCKER_USER)/$(IMAGE_NAME)

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

## docker-build: Build the error-logger Docker image with version tags
docker-build:
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest -t error-logger:latest .

## docker-tag: Tag image with major and latest aliases
docker-tag:
	docker tag $(IMAGE):$(VERSION) $(IMAGE):$(MAJOR)
	docker tag $(IMAGE):$(VERSION) $(IMAGE):latest

## docker-push: Push version, major, and latest tags to Docker Hub
docker-push: docker-tag
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):$(MAJOR)
	docker push $(IMAGE):latest

## docker-release: Build and push multi-arch images (amd64, arm64) to Docker Hub
docker-release:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):$(MAJOR) \
		-t $(IMAGE):latest \
		--push .

## docker-up: Start error-logger in background with Docker Compose
docker-up:
	docker compose up --build

## docker-down: Stop and remove Docker Compose containers
docker-down:
	docker compose down


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

