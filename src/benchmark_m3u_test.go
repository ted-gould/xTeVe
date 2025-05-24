package src_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv" // Added import for strconv
	"testing"

	m3uParser "xteve/src/internal/m3u-parser"
	xteveSrc "xteve/src"
)

var testdataDir = "testdata/benchmark_m3u"

// generateM3UContent dynamically creates M3U content as a byte slice.
func generateM3UContent(numEntries int, numGroups int) []byte {
	var buffer bytes.Buffer
	buffer.WriteString("#EXTM3U\n")

	for i := 1; i <= numEntries; i++ {
		groupID := ((i - 1) % numGroups) + 1 // Corrected groupID logic for 1-based indexing
		channelName := fmt.Sprintf("Channel Name %d", i)
		tvgID := fmt.Sprintf("id.%d", i)
		tvgLogo := fmt.Sprintf("http://logo.example.com/%d.png", i)
		groupTitle := fmt.Sprintf("Group Title %d", groupID)
		streamURL := fmt.Sprintf("http://stream.example.com/stream/%d", i)

		buffer.WriteString(fmt.Sprintf("#EXTINF:-1 tvg-id=\"%s\" tvg-name=\"%s\" tvg-logo=\"%s\" group-title=\"%s\",%s\n",
			tvgID, channelName, tvgLogo, groupTitle, channelName))
		buffer.WriteString(fmt.Sprintf("%s\n", streamURL))
	}
	return buffer.Bytes()
}

func BenchmarkParseM3U(b *testing.B) {
	fileSizes := []struct {
		name       string
		numEntries int // 0 means read from file
		numGroups  int
	}{
		{name: "small", numEntries: 0, numGroups: 0}, // Reads from small.m3u
		{name: "medium", numEntries: 1000, numGroups: 50},
		{name: "large", numEntries: 10000, numGroups: 100},
	}

	for _, sizeInfo := range fileSizes {
		b.Run(sizeInfo.name, func(b *testing.B) {
			var content []byte
			var err error

			if sizeInfo.numEntries == 0 { // Read from file for "small"
				filePath := filepath.Join(testdataDir, sizeInfo.name+".m3u")
				content, err = os.ReadFile(filePath)
				if err != nil {
					b.Fatalf("Failed to read file %s: %v", filePath, err)
				}
			} else { // Generate content for "medium" and "large"
				content = generateM3UContent(sizeInfo.numEntries, sizeInfo.numGroups)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := m3uParser.MakeInterfaceFromM3U(content)
				if err != nil {
					b.Fatalf("Error parsing M3U data for %s: %v", sizeInfo.name, err)
				}
			}
		})
	}
}

func BenchmarkFilterM3U(b *testing.B) {
	benchmarkCases := []struct {
		name       string
		numEntries int // 0 means read from file for initial parse
		numGroups  int
	}{
		{name: "small", numEntries: 0, numGroups: 0}, // Reads from small.m3u
		{name: "medium", numEntries: 1000, numGroups: 50},
		{name: "large", numEntries: 10000, numGroups: 100},
	}
	filterCounts := []int{1, 5, 10}

	sampleFilters := []xteveSrc.Filter{
		{Rule: "Group Title 1", CaseSensitive: false, Type: "group-title", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Channel Name 10", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "logo.example.com", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Group Title 20", CaseSensitive: false, Type: "group-title", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Channel Name 500", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "id.75", CaseSensitive: true, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Group Title 30", CaseSensitive: false, Type: "group-title", PreserveMapping: false, StartingChannel: ""},
		{Rule: "stream.example.com/stream/100", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Channel Name 999", CaseSensitive: false, Type: "custom-filter", PreserveMapping: false, StartingChannel: ""},
		{Rule: "Group Title 45", CaseSensitive: false, Type: "group-title", PreserveMapping: false, StartingChannel: ""},
	}

	for _, caseInfo := range benchmarkCases {
		b.Run(caseInfo.name, func(b *testing.B) {
			var m3uData []byte
			var err error

			if caseInfo.numEntries == 0 { // Read from file for "small"
				filePath := filepath.Join(testdataDir, caseInfo.name+".m3u")
				m3uData, err = os.ReadFile(filePath)
				if err != nil {
					b.Fatalf("Failed to read file %s: %v", filePath, err)
				}
			} else { // Generate content for "medium" and "large"
				m3uData = generateM3UContent(caseInfo.numEntries, caseInfo.numGroups)
			}

			parsedM3UInterface, err := m3uParser.MakeInterfaceFromM3U(m3uData)
			if err != nil {
				b.Fatalf("Error parsing M3U data for %s for filtering benchmark: %v", caseInfo.name, err)
			}

			var parsedM3U []map[string]string
			for _, item := range parsedM3UInterface {
				if streamMap, ok := item.(map[string]string); ok {
					parsedM3U = append(parsedM3U, streamMap)
				} else {
					b.Fatalf("Parsed M3U item is not a map[string]string for %s: %T", caseInfo.name, item)
				}
			}

			for _, numFilters := range filterCounts {
				b.Run(strconv.Itoa(numFilters)+"_filters", func(b *testing.B) {
					if numFilters > len(sampleFilters) {
						b.Skipf("Not enough sample filters defined for %d filters", numFilters)
					}

					currentFilters := make([]xteveSrc.Filter, numFilters)
					copy(currentFilters, sampleFilters[:numFilters])
					xteveSrc.Data.Filter = currentFilters

					b.ReportAllocs()
					b.ResetTimer()

					for i := 0; i < b.N; i++ {
						for _, streamMap := range parsedM3U {
							_ = xteveSrc.FilterThisStream(streamMap)
						}
					}
				})
			}
		})
	}
}
