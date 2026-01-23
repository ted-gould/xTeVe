## Testing Teddy Journal

This journal documents critical learnings about the test suite.

## 2025-01-21 - [Initial Audit] **Learning:** The `src` package has entangled dependencies making isolated testing difficult. **Action:** Run tests with `go test ./src/...` instead of individual files to ensure all dependencies are resolved.

## 2026-01-23 - [Generated Code Dependencies] **Learning:** The m3u-parser package requires `make generate` or manual `go generate` with proper PATH to create regexp2go files before tests can run. **Action:** Always check for `//go:generate` directives and run generation steps before testing. Generated files are in .gitignore so must be recreated locally.
