## 2026-01-11 - In-place Sorting with Pointers
**Learning:** When sorting a slice using a temporary helper structure with pointers to the original elements, NEVER rebuild the original slice in-place by dereferencing those pointers. If you overwrite the original slice while reading from it (via pointers), you will corrupt the data because the pointers refer to memory locations that are being modified.
**Action:** Always allocate a new slice for the result when rebuilding from a temporary sorted structure that points to the original data, or use indices carefully if you must do it in-place (though swapping large structs is expensive anyway).

## 2026-01-26 - URL Parsing Optimization
**Learning:** `url.ParseRequestURI` and `url.Parse` are relatively expensive (~300-1000ns) and generate allocations. For simple prefix checks (like identifying absolute URLs or paths), `strings.HasPrefix` (~5ns) is vastly superior.
**Action:** When iterating over thousands of items (like playlist segments), avoid parsing URLs if a simple string check suffices to classify the URL type.

## 2026-01-31 - Zero-Allocation Hashing
**Learning:** `hash/maphash` is significantly faster (6-7x) than `crypto/md5` for internal hash map keys, especially when hashing multiple string components. It supports `WriteString` natively (avoiding `[]byte` allocation from string conversion) and produces `uint64` keys which are faster for map lookups than strings.
**Action:** For internal deduplication or indexing maps where persistence is not required (as `maphash` is seeded per-process), prefer `maphash` over cryptographic hashes or string concatenation.
