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
