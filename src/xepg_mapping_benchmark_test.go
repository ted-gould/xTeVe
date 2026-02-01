package src

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkPerformAutomaticChannelMapping benchmarks the performance of mapping logic,
// focusing on the nested loop where string manipulation happens.
func BenchmarkPerformAutomaticChannelMapping(b *testing.B) {
	// Setup global state similar to tests
	originalData := Data
	defer func() { Data = originalData }()

	Data = DataStruct{
		XMLTV: struct {
			Files   []string
			Mapping map[string]map[string]XMLTVChannelMapping
		}{
			Mapping: make(map[string]map[string]XMLTVChannelMapping),
		},
	}

	// Create a large mapping to simulate heavy workload
	// 5 providers, 2000 channels each, 5 display names each
	// This results in 5 * 2000 * 5 = 50,000 string comparisons per call in worst case
	numProviders := 5
	channelsPerProvider := 2000
	displayNamesPerChannel := 5

	for p := 0; p < numProviders; p++ {
		providerName := fmt.Sprintf("provider_%d.xml", p)
		Data.XMLTV.Mapping[providerName] = make(map[string]XMLTVChannelMapping)

		for c := 0; c < channelsPerProvider; c++ {
			channelID := fmt.Sprintf("ch_%d_%d", p, c)
			var displayNames []DisplayName
			for d := 0; d < displayNamesPerChannel; d++ {
				displayNames = append(displayNames, DisplayName{
					Value: fmt.Sprintf("Channel Name %d %d Variant %d", p, c, d),
				})
			}

			Data.XMLTV.Mapping[providerName][channelID] = XMLTVChannelMapping{
				ID:           channelID,
				DisplayNames: displayNames,
			}
		}
	}

	// The channel we are trying to map (that will trigger the fallback search)
	// We choose a name that matches the very last entry to ensure we scan everything.
	lastProvider := numProviders - 1
	lastChannel := channelsPerProvider - 1
	lastVariant := displayNamesPerChannel - 1
	targetName := fmt.Sprintf("Channel Name %d %d Variant %d", lastProvider, lastChannel, lastVariant)

	targetChannel := XEPGChannelStruct{
		Name:      targetName,
		TvgID:     "nomatch",
		XmltvFile: "",
		XMapping:  "",
	}

	// Also ensure Settings allow mapping
	originalSettings := Settings
	defer func() { Settings = originalSettings }()
	Settings.DefaultMissingEPG = "-" // Do not use default dummy, force search

	// Build Index
	var xmltvNameIndex = make(map[string]map[string]string)
	for file, xmltvChannels := range Data.XMLTV.Mapping {
		xmltvNameIndex[file] = make(map[string]string)
		for _, channel := range xmltvChannels {
			for _, displayName := range channel.DisplayNames {
				normalizedName := strings.ToLower(strings.ReplaceAll(displayName.Value, " ", ""))
				if _, exists := xmltvNameIndex[file][normalizedName]; !exists {
					xmltvNameIndex[file][normalizedName] = channel.ID
				}
			}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = performAutomaticChannelMapping(targetChannel, "testID", xmltvNameIndex)
	}
}
