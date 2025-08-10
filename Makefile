.PHONY: all build test js clean

# Default target
all: build

# Build the application
build: js
	@echo "Building Go commands..."
	@mkdir -p bin
	@go build -v -o ./bin/xteve .
	@go build -v -o ./bin/xteve-inactive ./cmd/xteve-inactive
	@go build -v -o ./bin/xteve-status ./cmd/xteve-status

# Run tests
test: js
	@echo "Running Go tests..."
	@go test ./...

# Compile TypeScript
js:
	@echo "Compiling TypeScript..."
	@(cd ts && sh compileJS.sh)

# Clean up build artifacts
clean:
	@echo "Cleaning up..."
	@rm -rf bin
	@rm -rf src/html/js
