## 2025-02-23 - XMLTV Time Processing Optimization

**Learning:** String manipulation functions like `strings.Split` and `strings.Join` in hot loops (processing thousands of XMLTV programs) cause significant allocation churn. Even simple splitting of a string to parse a timezone creates unnecessary slices and strings.

**Action:**
1.  **Fast Path:** Identify the common case (e.g., `timeshift == 0`) and bypass processing entirely if possible.
2.  **Zero-Allocation Parsing:** Use `strings.Cut` or `strings.IndexByte` instead of `strings.Split` to find delimiters without allocating a slice.
3.  **Direct Construction:** Use concatenation for simple string assembly instead of `fmt.Sprintf` or `strings.Join`.

**Impact:** Reduced allocations from 3 per op to 0 per op in the common case (146x speedup), and reduced allocations by ~33% in the processing case.
