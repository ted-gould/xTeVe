## 2026-01-18 - Streaming M3U Generation

**Learning:** Generating large M3U playlists using `strings.Builder` followed by `sb.String()` and `[]byte(str)` caused massive triple-allocation (builder buffer, string copy, byte slice copy). This spiked memory usage to ~40MB/op for 10k channels.

**Action:** Refactored to `buildM3UToWriter(w io.Writer)` to stream output directly to `http.ResponseWriter` or `os.File` (via `bufio.Writer`). This reduced allocations to ~6.4MB/op (83% reduction) and eliminated the large contiguous heap allocations. Also optimized sorting to avoid rebuilding the slice.
