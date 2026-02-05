## 2025-02-23 - XMLTV Time Processing Optimization

**Learning:** String manipulation functions like `strings.Split` and `strings.Join` in hot loops (processing thousands of XMLTV programs) cause significant allocation churn. Even simple splitting of a string to parse a timezone creates unnecessary slices and strings.

**Action:**
1.  **Fast Path:** Identify the common case (e.g., `timeshift == 0`) and bypass processing entirely if possible.
2.  **Zero-Allocation Parsing:** Use `strings.Cut` or `strings.IndexByte` instead of `strings.Split` to find delimiters without allocating a slice.
3.  **Direct Construction:** Use concatenation for simple string assembly instead of `fmt.Sprintf` or `strings.Join`.

**Impact:** Reduced allocations from 3 per op to 0 per op in the common case (146x speedup), and reduced allocations by ~33% in the processing case.

## 2026-01-22 - XMLTV Category Slice Reuse

**Learning:** `getCategory` in `xepg.go` allocated a new slice for every program to copy categories from the source, even when no modification was needed. This caused N extra allocations for N programs.

**Action:**
1.  **Reuse Immutable Slices:** Check if the modification (adding `xCategory`) is actually needed.
2.  **Aliasing:** If `xCategory` is empty, assign the source slice directly (`program.Category = xmltvProgram.Category`) instead of making a copy. This is safe because the source is effectively immutable during this operation and the destination is write-once (for XML marshaling).

**Impact:** Reduced allocations by 1000 per 1000 ops (33% reduction in `getProgramData` allocation count).

## 2026-01-22 - XEPG Database Rebuild Allocations

**Learning:** `createXEPGDatabase` in `src/xepg.go` contained a redundant map initialization (`make(map...)`) that was immediately overwritten by a function return value. Additionally, slices and maps used for indexing were growing dynamically inside loops despite the source size being known.

**Action:**
1.  **Remove Redundant Make:** Eliminated `Data.XEPG.Channels = make(...)` as it was dead code (overwritten next line).
2.  **Pre-allocate Slices/Maps:** Initialized `allChannelNumbers` (slice) and `xepgChannelsValuesMap` (map) with `cap(len(Data.XEPG.Channels))` to eliminate growth reallocation penalties.

**Impact:** Reduced allocation count and GC pressure during the database rebuild phase (O(N) growth allocations -> O(1) allocation).

## 2026-01-25 - [Hash Writer Interface Allocations] **Learning:** Using `io.WriteString(h, s)` where `h` is a `hash.Hash` (interface) causes `[]byte(s)` to allocate because `crypto/md5` does not implement `io.StringWriter`, and passing the slice to the `Write` interface method forces it to escape (or at least allocate). String concatenation + single `[]byte` conversion was significantly faster (3 allocs vs 11 allocs). **Action:** Avoid `io.WriteString` on `hash.Hash` for many small strings; prefer concatenation or `unsafe` if critical, or accept that `md5.Sum` is already optimized.

## 2026-01-27 - Debug Logging Pre-formatting Allocations

**Learning:** `debug = fmt.Sprintf(...)` followed by `showDebug(debug, level)` allocates the string *before* the level check inside `showDebug`. This causes significant allocation overhead in hot paths even when debug logging is disabled.

**Action:** Wrap the `fmt.Sprintf` call in an explicit check for the debug level (e.g., `if System.Flag.Debug >= level { ... }`) to avoid formatting and allocation when not needed.

**Impact:** Reduced allocations in `ParseM3U8` by 1 per call (huge if body is large) when debug is off.

## 2026-01-27 - bufio.Scanner vs String Slicing

**Learning:** `bufio.Scanner` allocates an internal buffer (initially 4KB) and wraps the reader. For parsing strings already in memory, iterating via `strings.IndexByte` and slicing is zero-allocation and significantly faster.

**Action:** Replace `bufio.Scanner` with a manual loop using `strings.IndexByte` when parsing in-memory strings.

## 2026-02-05 - Ephemeral Hash Map Optimization

**Learning:** `generateChannelHash` used `md5.Sum` with string concatenation (`s1 + s2 + ...`) and `hex.EncodeToString`. This caused 3 allocations per call (string concat, byte slice conversion, hex string). Since the hash was only used for a local map (`xepgChannelsValuesMap`) within a single function scope, cryptographic persistence wasn't needed.

**Action:** Replaced `md5` with `hash/maphash`. Passed a reused `*maphash.Hash` to the generator function. Changed map key from `string` to `uint64`.

**Impact:** Allocations reduced from 3 to 0. Time reduced by ~11x (790ns -> 71ns).
