## 2026-01-11 - In-place Sorting with Pointers
**Learning:** When sorting a slice using a temporary helper structure with pointers to the original elements, NEVER rebuild the original slice in-place by dereferencing those pointers. If you overwrite the original slice while reading from it (via pointers), you will corrupt the data because the pointers refer to memory locations that are being modified.
**Action:** Always allocate a new slice for the result when rebuilding from a temporary sorted structure that points to the original data, or use indices carefully if you must do it in-place (though swapping large structs is expensive anyway).

## 2026-01-26 - URL Parsing Optimization
**Learning:** `url.ParseRequestURI` and `url.Parse` are relatively expensive (~300-1000ns) and generate allocations. For simple prefix checks (like identifying absolute URLs or paths), `strings.HasPrefix` (~5ns) is vastly superior.
**Action:** When iterating over thousands of items (like playlist segments), avoid parsing URLs if a simple string check suffices to classify the URL type.

## 2026-02-14 - Redundant Scanning in Parsing
**Learning:** Scanning a large string (e.g., `strings.Count(body, ...)`) to estimate capacity for pre-allocation is wasteful if the allocation is later discarded or re-done. In `ParseM3U8`, we were scanning the entire body twice for segments count. Removing the first scan improved performance by ~14% and reduced memory usage by ~35%.
**Action:** Verify if pre-calculation of capacity is actually used and not overwritten later in the code path. Avoid multiple O(N) scans of large inputs.

## 2026-03-07 - UTF-8 Case Folding in Custom String Functions
**Learning:** When optimizing string comparisons in Go (e.g., ignoring spaces without allocating new strings), simple ASCII case-folding logic will break on international characters (like EPG channel names containing 'Ö', 'É', etc.). EPG data frequently contains non-ASCII characters.
**Action:** Always use `utf8.DecodeRuneInString` and `unicode.SimpleFold` (or `unicode.ToLower`) to implement custom case-insensitive matching that accurately mirrors `strings.EqualFold()`, ensuring both correctness and zero-allocation performance.

## 2024-04-04 - [Single-Pass String Operations]
**Learning:** In performance-critical paths (like XEPG channel mapping), chaining standard library string operations (e.g., `strings.ToLower(strings.ReplaceAll(...))`) causes unnecessary intermediate string allocations.
**Action:** Use single-pass helper functions with `strings.Builder` (like `toLowerReplaceSpace`) or allocation-free comparison functions (like `equalFoldNoSpaces`) for string manipulation in hot loops.
