# Instructions for Agents

This file consolidates critical learnings, architectural decisions, security guidelines, and performance optimizations for the codebase. Always consult this file before making changes.

## Environment & Build
* **Prerequisites**:
    * **Go version**: 1.25 or newer is required.
    * **FFmpeg**: Required for streaming functionality. Install with `sudo apt-get update && sudo apt-get install -y ffmpeg`.
    * **Packages**: Always use `sudo` for `apt-get` commands.
* **Building**:
    * Run `make build` to compile backend and frontend. Output binaries are in `bin/`.
    * **Always run `make build` before running tests** to ensure embedded assets (e.g., compiled TypeScript, generated Go code) are up-to-date.
* **Artifacts & Scope**:
    * **Do not edit**: Files in `bin/`, `build/`, `dist/`, `node_modules/`, or generated files like `src/internal/m3u-parser/regexp2go_*.go`.
    * **Edit Source**: Modify code in `src/`, `cmd/`, or `ts/`.
    * **Scope**: Instructions in this file apply to the entire repository.

## Architecture & Logic
* **XEPG & XMLTV**:
    * **Streaming Pattern**: Use the `yield` callback pattern (`func(*Program) error`) instead of appending to slices for large datasets (e.g., `getProgramData`).
    * **Atomic Writes**: When generating files (like XMLTV), write to a temporary file (`.tmp`) first and rename upon success to ensure data integrity.
    * **Optimization**:
        * `createXEPGDatabase` uses ID-based indexing (strings) for maps to reduce heap allocations.
        * `performAutomaticChannelMapping` uses `xmltvNameIndex` for O(1) lookups.
* **WebDAV**:
    * **Plex Naming**: `generateFileStreamInfos` enforces strict naming (`Series - SXXEXX`) and sanitizes filenames (spaces preserved, slashes replaced).
    * **Image Handling**: `uploadLogo` validates extensions against an allowlist and uses `filepath.Base` for sanitization.
    * **Modification Time Logic**:
        * **Priority Order**:
            1. **JSON Cache**: Check the `mod_time` in the `filecache` JSON sidecar file.
            2. **M3U Internal**: Check M3U attributes (e.g., `time`, `date`, `mtime`).
            3. **HTTP HEAD**: Perform a HEAD request to the stream URL.
            4. **HTTP GET (Partial)**: Perform a partial GET request (first 1MB) and check headers.
            5. **JSON File Stat**: Use the modification time of the `filecache` JSON file itself.
            6. **M3U File Stat**: Use the modification time of the source M3U file.
        * **Persistence**: The determined modification time is always written back to the JSON cache file to ensure consistency.
* **M3U8 Parser**:
    * **Pre-allocation**: `ParseM3U8` uses `strings.Count` to estimate segment count and pre-allocate slices.
* **Concurrency**:
    * **SSDP**: Manages multiple advertisers (root, WebDAV) in a single goroutine.
    * **Buffers**: `handleTSStream` reuses a single pre-allocated buffer for packet processing to minimize GC.

## Security & Robustness
* **Authentication**:
    * **Timing Attacks**: `UserAuthentication` iterates all registered users regardless of a match and uses `crypto/subtle.ConstantTimeCompare`.
    * **Passwords**: Use `bcrypt`. Legacy HMAC-SHA256 passwords are lazily migrated to bcrypt upon successful login.
    * **URL Auth**: Credentials can be extracted from URL query parameters (`username`, `password`).
    * **Token Validation**: `CheckTheValidityOfTheTokenFromHTTPHeader` retrieves tokens via `r.Cookie("Token")`.
* **Input Validation**:
    * **File Uploads**: Strictly validate file extensions and content types. Sanitize filenames to prevent path traversal.
    * **Network**:
        * **SSRF**: Block loopback and link-local addresses.
        * **IP Resolution**: `getClientIP` prioritizes `X-Real-IP` then `X-Forwarded-For` only when the request originates from a private/loopback address.
* **DoS Prevention**:
    * **WebSockets**: Enforce a 32MB read limit (`conn.SetReadLimit(33554432)`).
    * **File Downloads**: Stream files using `io.Copy` or `http.ServeFile`; do not read entire files into memory.
    * **Rate Limiting**: Use Fixed Window or Token Bucket algorithms; avoid naive "last seen" updates that ban legitimate active users.

## Performance & Memory Optimization
* **Allocations**:
    * **Strings**: Use `strings.IndexByte`, `strings.Cut`, or `strings.HasPrefix` instead of `strings.Split`, `strings.Join`, or `url.Parse` in hot loops.
    * **Slices/Maps**: Always pre-allocate with `make(..., size)` or `cap(len(...))` if the size is known.
    * **Zero-Allocation**:
        * Use `parser.NextInto` to parse directly into a caller-supplied buffer.
        * Use `hash/maphash` (with `uint64` keys) for ephemeral hashing instead of `crypto/md5`.
* **Patterns**:
    * **Logging**: Wrap `fmt.Sprintf` calls in debug level checks (`if System.Flag.Debug >= level`) to avoid formatting overhead when disabled.
    * **Standard Lib**: Use the `slices` and `maps` packages (Go 1.21+) for operations.
    * **Equality**: Use `github.com/google/go-cmp` for comparisons.
    * **Sorting**: When sorting with pointers, allocate a new slice for the result; do not rebuild in-place.

## Testing
* **State Isolation**:
    * Use `t.Cleanup` to restore global state (`Settings`, `System`) after tests.
    * Explicitly reset singletons like `filecache.Reset()`.
    * Tests involving `authentication.Init` must use `os.MkdirTemp` for isolation.
* **Integration**:
    * Run integration tests in `cmd/e2e-streaming-test`.
    * Mock WebDAV data using `map[string]string`.
* **Generated Code**:
    * Run `make generate` to install dependencies (`regexp2go`, `bytespool`) and generate code before running tests.
    * Verify `//go:generate` directives are respected.
