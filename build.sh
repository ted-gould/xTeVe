#!/bin/bash
set -e

echo "--- Compiling TypeScript ---"
(cd ts && sh compileJS.sh)

echo "--- Building Go commands ---"
mkdir -p bin
go build -v -o ./bin/xteve .
go build -v -o ./bin/xteve-inactive ./cmd/xteve-inactive
go build -v -o ./bin/xteve-status ./cmd/xteve-status

echo "--- Build complete ---"
