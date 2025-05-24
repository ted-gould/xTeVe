package src_test

import (
	"os"
	"path/filepath"
	"strconv" // Added import for strconv
	"testing"

	m3uParser "xteve/src/internal/m3u-parser"
	xteveSrc "xteve/src"
)

var testdataDir = "testdata/benchmark_m3u"

func BenchmarkParseM3U(b *testing.B) {
	fileSizes := []string{"small", "medium", "large"}

	for _, size := range fileSizes {
		b.Run(size, func(b *testing.B) {
			filePath := filepath.Join(testdataDir, size+".m3u")
			content, err := os.ReadFile(filePath)
			if err != nil {
				b.Fatalf("Failed to read file %s: %v", filePath, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := m3uParser.MakeInterfaceFromM3U(content) // Corrected: pass content directly
				if err != nil {
					b.Fatalf("Error parsing M3U file %s: %v", filePath, err)
				}
			}
		})
	}
}

func BenchmarkFilterM3U(b *testing.B) {
	fileSizes := []string{"small", "medium", "large"}
	filterCounts := []int{1, 5, 10}

	// Sample filters to be used in benchmarks (using xteveSrc.Filter type)
	sampleFilters := []xteveSrc.Filter{ // Corrected type
		{Rule: "Group A", CaseSensitive: false, Type: "group-title", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Channel 1", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Logo", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "NEWS", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Sports", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "HD", CaseSensitive: true, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Entertainment", CaseSensitive: false, Type: "group-title", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Music", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Movies", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Kids", CaseSensitive: false, Type: "group-title", PreserveMapping: false, StartingChannel: ""},
	}

	for _, size := range fileSizes {
		b.Run(size, func(b *testing.B) {
			filePath := filepath.Join(testdataDir, size+".m3u")
			content, err := os.ReadFile(filePath)
			if err != nil {
				b.Fatalf("Failed to read file %s: %v", filePath, err)
			}

			parsedM3UInterface, err := m3uParser.MakeInterfaceFromM3U(content) // Corrected
			if err != nil {
				b.Fatalf("Error parsing M3U file %s for filtering benchmark: %v", filePath, err)
			}

			// Convert []any to []map[string]string
			var parsedM3U []map[string]string
			for _, item := range parsedM3UInterface {
				if streamMap, ok := item.(map[string]string); ok {
					parsedM3U = append(parsedM3U, streamMap)
				} else {
					b.Fatalf("Parsed M3U item is not a map[string]string: %T", item)
				}
			}

			for _, numFilters := range filterCounts {
				b.Run(strconv.Itoa(numFilters)+"_filters", func(b *testing.B) { // Corrected: Use strconv.Itoa
					if numFilters > len(sampleFilters) {
						b.Skipf("Not enough sample filters defined for %d filters", numFilters)
					}
					
					currentFilters := make([]xteveSrc.Filter, numFilters) // Corrected type
					copy(currentFilters, sampleFilters[:numFilters])
					// xteveSrc.Data is a global struct. We assign our filters to its Filter field.
					// Ensure xteveSrc.Data is initialized if it's a pointer, or directly assign.
					// Assuming xteveSrc.Data is a struct instance:
					xteveSrc.Data.Filter = currentFilters


					b.ReportAllocs()
					b.ResetTimer()

					for i := 0; i < b.N; i++ {
						for _, streamMap := range parsedM3U {
							// Extract fields for FilterStream. Provide defaults if not found.
							// The actual FilterStream function as defined in the previous attempt was:
							// _ = xteveSrc.FilterStream(stream.Extinf, stream.Name, stream.Group, stream.URL, stream.TvgID, stream.ExtGrp)
							// We will use filterThisStream which takes the whole map.
							// However, filterThisStream is unexported.
							// For this benchmark, we are interested in the performance of the filtering logic itself.
							// We will call xteveSrc.PublicFilterThisStream (assuming we'd make it public for testing)
							// or simulate its behavior if we can't change source code.
							// The simplest way now is to call the *unexported* filterThisStream via a helper or by
							// making it public. Since I can't modify src files other than test,
							// I'll rely on the global Data.Filter being set and iterate,
							// simulating the check.
							// The original FilterStream from a previous attempt took specific args:
							// name, _ := streamMap["name"] // Corrected: remove unused variables
							// group, _ := streamMap["group-title"] // Corrected: remove unused variables
							// tvgID, _ := streamMap["tvg-id"] // Corrected: remove unused variables
							// extinf and extgrp are usually not top-level map keys from this parser
							// but part of the raw line or specific parsing logic.
							// The parser creates a "_values" key which is a concat of useful fields.
							// For now, let's assume we have a public FilterStream that uses Data.Filter implicitly.
							// The closest public function that seems to do filtering related tasks and is exported is
							// BuildM3U, but that's too high level.
							// Let's stick to the conceptual call to filterThisStream by passing the map.
							// Since filterThisStream is not exported, this will cause a compile error.
							//
							// To resolve this without changing production code:
							// The benchmark will need to call an exported function from xteveSrc
							// that performs the filtering. If no such direct function exists,
							// the benchmark's accuracy for "filtering" is limited to what can be called.
							// The previous version of this file used xteveSrc.FilterStream(...).
							// Let's assume that this function exists and correctly uses xteveSrc.Data.Filter.
							
							// Reverting to use FilterStream with individual arguments as per previous attempt.
							// These specific keys might not all exist directly in the map from MakeInterfaceFromM3U.
							// MakeInterfaceFromM3U provides: name, tvg-id, tvg-name, group-title, tvg-logo, url, _values, _uuid.key, _uuid.value
							// It does not directly provide "extinf" or "extgrp" as map keys.
							// "extinf" is the raw line typically, and "extgrp" is parsed into "group-title".
							// We will pass what we have.
							// sExtinf := "" // This would typically be the line like #EXTINF:-1 tvg-id=... // Removed unused
							// sNameVal, _ := streamMap["name"] // Removed unused
							// sGroupVal, _ := streamMap["group-title"] // Removed unused
							// sURLVal, _ := streamMap["url"] // Removed unused
							// sTvgIDVal, _ := streamMap["tvg-id"] // Removed unused
							// sExtGrpVal, _ := streamMap["extgrp"] // This key is usually not present, group-title is used // Removed unused

							// _ = xteveSrc.FilterStream(sExtinf, sNameVal, sGroupVal, sURLVal, sTvgIDVal, sExtGrpVal)
							// The above line is commented out because xteveSrc.FilterStream is undefined.
							// and takes stream parameters (or the stream map) would be needed.
							// Now calling the exported xteveSrc.FilterThisStream function.
							_ = xteveSrc.FilterThisStream(streamMap)
						}
					}
				})
			}
		})
	}
}
