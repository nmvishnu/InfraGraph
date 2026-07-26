# InfraGraph Makefile

BINARY   := infragraph
CMD_PATH := ./cmd/infragraph
BUILD_DIR := ./build
NEO4J_URI := bolt://localhost:7687
NEO4J_USER := neo4j
NEO4J_PASS := infragraph

.PHONY: all build run start-db stop-db clean deps install help

## Default: build the binary
all: build

## Build the Go binary
build:
	@echo "[build] Compiling infragraph..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) $(CMD_PATH)
	@echo "[build] Binary at $(BUILD_DIR)/$(BINARY)"

## Install binary to GOPATH/bin
install:
	go install $(CMD_PATH)
	@echo "[install] infragraph installed to $$(go env GOPATH)/bin/"

## Download Go dependencies
deps:
	go mod tidy

## Start Neo4j via Docker Compose
start-db:
	@echo "[db] Starting Neo4j container..."
	$${COMPOSE:-docker compose} -f docker/docker-compose.yml up -d neo4j
	@echo "[db] Neo4j Browser → http://localhost:7474  (neo4j / infragraph)"

## Stop Neo4j
stop-db:
	$${COMPOSE:-docker compose} -f docker/docker-compose.yml down

## Run a full scan + serve UI (set TARGET=yourdomain.com)
run: build
	@if [ -z "$(TARGET)" ]; then \
		echo "Usage: make run TARGET=example.com"; exit 1; \
	fi
	$(BUILD_DIR)/$(BINARY) \
		-target $(TARGET) \
		-neo4j $(NEO4J_URI) \
		-user $(NEO4J_USER) \
		-pass $(NEO4J_PASS) \
		-output ./output \
		-web ./web

## Ingest existing JSON files without re-scanning
ingest: build
	$(BUILD_DIR)/$(BINARY) \
		-ingest-only \
		-neo4j $(NEO4J_URI) \
		-user $(NEO4J_USER) \
		-pass $(NEO4J_PASS) \
		-output ./output \
		-web ./web

## Only serve the UI (no scan or ingest)
serve: build
	$(BUILD_DIR)/$(BINARY) \
		-serve-only \
		-neo4j $(NEO4J_URI) \
		-user $(NEO4J_USER) \
		-pass $(NEO4J_PASS) \
		-web ./web

## Build + verify compilation
check:
	go vet ./...
	go build ./...
	@echo "[check] All good!"

## Remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## Help
help:
	@grep -E '^## ' Makefile | sed 's/^## //'
