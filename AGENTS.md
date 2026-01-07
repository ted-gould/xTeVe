# Instructions for Jules

## Environment & Build
* **Prerequisites**:
    * Go version 1.24 or newer is required.
    * `ffmpeg` is required. Install with `sudo apt-get update && sudo apt-get install -y ffmpeg`.
    * Always use `sudo` when running `apt-get` commands for package installation or system updates.
* **Building**:
    * Run `make build` to compile the backend and frontend. Output binaries are placed in `bin/` (`xteve`, `xteve-inactive`, `xteve-status`).
    * Always run `make build` before running tests to ensure embedded assets (like compiled TypeScript) are generated.
* **Artifacts**:
    * Do not edit files in `bin/`, `build/`, `dist/`, or `node_modules/`. Edit the source code in `src/`, `cmd/`, or `ts/`.
    * `AGENTS.md` scope applies to the entire repository.
* **Environment**:
    * When creating a new VM, run `make build` and fix dependency issues until it passes. This ensures the development environment is ready.

## Testing
* **General**:
    * For any new code, create a test.
    * When fixing a bug, first write a failing test to reproduce the bug, then fix it, then verify the test passes.
    * Run all tests with `go test ./...`.
* **State Isolation**:
    * Tests involving `filecache` must explicitly reset the singleton using `filecache.Reset()` (or `resetFileCache()` in `src` package tests).
    * Tests involving authentication must reset global variables (`data`, `tokens`).
    * Integration tests using global buffers (`BufferClients`, `BufferInformation`) must use unique IDs or explicit cleanup.
* **Specifics**:
    * Integration tests are located in `cmd/e2e-streaming-test`.
    * Use package path (e.g., `go test -v ./src/ -run <TestName>`) for focused tests to ensure internal dependencies resolve correctly.
    * Mock WebDAV stream data using `map[string]string`, not `map[string]interface{}`.
    * The benchmark test `src/benchmark_m3u_test.go` requires `src/testdata/benchmark_m3u/small.m3u`.

## Coding Standards & Patterns
* **Quality**:
    * Prioritize code readability and maintainability.
* **Error Handling**:
    * Strictly propagate errors. Check errors from IO/OS calls (e.g., `Write`, `Seek`). Use `_` if explicitly ignoring.
* **Performance**:
    * Use `strings.Builder` with `WriteString` for large text generation (e.g., M3U playlists).
    * Use `bindToStruct` for converting maps/objects to structs via JSON (avoids allocations). Avoid `mapToJSON` for internal logic.
    * Use the standard library `slices` package (Go 1.21+) for slice operations instead of `sort` or third-party libs.
* **Context & Tracing**:
    * Propagate `context.Context` in WebDAV and streaming functions to ensure OpenTelemetry spans are correctly inherited.
    * Use `context.WithoutCancel` if a background task must survive the parent request's cancellation.
* **Formatting**:
    * Use constant format strings with `fmt.Sprintf` (e.g., `fmt.Sprintf("%s", val)`, not `fmt.Sprintf(val)`).

## Architecture & Logic
* **WebDAV**:
    * Logic resides in `src/webdav_fs.go`.
    * Implements virtual image files for `tvg-logo` streams.
    * Filenames are sanitized (replace `/` with `_`, preserve spaces).
    * Image filenames must mirror video filenames.
* **Middleware**:
    * Execution order: OpenTelemetry -> Security Headers -> Panic Recovery -> ServeMux.
* **Snap**:
    * Configuration is read from `$SNAP_COMMON/otel.env` at startup.
