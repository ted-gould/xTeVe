.PHONY: all build test js clean

# Default target
all: build

# Build the application
build: js
	@echo "Building Go application..."
	@go build -o xteve

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
	@rm -f xteve
	@rm -rf src/html/js
