# Go variables
GO = go
GOCMD = $(GO) build -v
BINS = xteve xteve-inactive xteve-status
BINDIR = bin

# Default target
all: build

# Build targets
build: ts-compile generate-video
	@echo "--- Building Go commands ---"
	@mkdir -p $(BINDIR)
	$(GOCMD) -o ./$(BINDIR)/xteve .
	$(GOCMD) -o ./$(BINDIR)/xteve-inactive ./cmd/xteve-inactive
	$(GOCMD) -o ./$(BINDIR)/xteve-status ./cmd/xteve-status
	@echo "--- Build complete ---"

ts-compile:
	@echo "--- Compiling TypeScript ---"
	(cd ts && npx tsc -p tsconfig.json)

generate-video:
	@echo "--- Generating video asset ---"
	@bash build/generate_video.sh

# Test and lint targets
test: ts-compile
	@echo "--- Running Go tests ---"
	$(GO) test ./...

lint:
	@echo "--- Running golangci-lint ---"
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(shell $(GO) env GOPATH)/bin/golangci-lint run

e2e-test: build
	@echo "--- Running E2E tests ---"
	$(GO) run cmd/ci-test/main.go

format-check:
	@echo "--- Checking formatting ---"
	npx prettier --check "**/*.{js,ts}"

# Snap target
snap: build
	@echo "--- Building snap ---"
	snapcraft --destructive-mode

# Clean target
clean:
	@echo "--- Cleaning up ---"
	@rm -rf $(BINDIR)
	@rm -f src/html/video/stream-limit.bin
	# Add other clean up commands if needed for TypeScript files

.PHONY: all build ts-compile generate-video test lint e2e-test format-check snap clean
