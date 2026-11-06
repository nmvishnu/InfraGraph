# InfraGraph Makefile

BINARY     := infragraph
CMD_PATH   := ./cmd/infragraph
BUILD_DIR  := ./build
DB_PATH    := ./infragraph.db

.PHONY: all build static run ingest serve clean deps check help

## Default: build the static binary
all: static

## Build binary (fast dev build)
build:
	@echo "[build] Compiling $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) $(CMD_PATH)
	@echo "[build] Binary at $(BUILD_DIR)/$(BINARY)"

## Build fully self-contained static binary (zero external CGO / dynamic deps)
static:
	@echo "[build] Compiling static self-contained binary..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY) $(CMD_PATH)
	@echo "[build] Static binary ready at $(BUILD_DIR)/$(BINARY)"

## Download & tidy Go dependencies
deps:
	go mod tidy

## Run a full scan + serve UI (usage: make run TARGET=example.com)
run: build
	@if [ -z "$(TARGET)" ]; then \
		echo "Usage: make run TARGET=example.com"; exit 1; \
	fi
	$(BUILD_DIR)/$(BINARY) \
		-target $(TARGET) \
		-db $(DB_PATH) \
		-output ./output

## Ingest existing JSON files without re-scanning
ingest: build
	$(BUILD_DIR)/$(BINARY) \
		-ingest-only \
		-db $(DB_PATH) \
		-output ./output

## Only serve the embedded UI (no scan or ingest)
serve: build
	$(BUILD_DIR)/$(BINARY) \
		-serve-only \
		-db $(DB_PATH)

## Build + verify compilation
check:
	go vet ./...
	go build ./...
	@echo "[check] All good!"

## Remove build artifacts & temp databases
clean:
	rm -rf $(BUILD_DIR)

## Help
help:
	@grep -E '^## ' Makefile | sed 's/^## //'
