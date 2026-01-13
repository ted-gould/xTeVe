package src

import (
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	m3u "xteve/src/internal/m3u-parser"

)

// Parse Playlists
func parsePlaylist(filename, fileType string) (channels []any, err error) {
	content, err := readByteFromFile(filename)
	var id = strings.TrimSuffix(getFilenameFromPath(filename), path.Ext(getFilenameFromPath(filename)))
	var playlistName = getProviderParameter(id, fileType, "name")

	if err == nil {
		switch fileType {
		case "m3u":
			channels, err = m3u.MakeInterfaceFromM3U(content)
		case "hdhr":
			channels, err = makeInteraceFromHDHR(content, playlistName, id)
		}
	}
	return
}

// Filter Streams
// FilterThisStream checks if a stream should be filtered based on global filter rules.
// It is used by benchmarks and potentially other parts of the application.
func FilterThisStream(s any) (status bool) {
	// status is false by default for a bool named return
	stream, ok := s.(map[string]string)
	if !ok {
		// This should ideally not happen if s is always map[string]string
		return false
	}

	// Cache raw stream values. Normalize _values once.
	rawStreamGroup, streamGroupOK := stream["group-title"]
	rawStreamValues, streamValuesOK := stream["_values"]
	if streamValuesOK {
		rawStreamValues = strings.Replace(rawStreamValues, "\r", "", -1)
	}

	// Lazy initialization vars
	var lowerStreamGroup string
	var lowerStreamValues string
	var lowerStreamGroupInit bool
	var lowerStreamValuesInit bool

	for _, filter := range Data.Filter {
		if filter.Rule == "" {
			continue
		}

		var match = false
		var searchTarget string // This will hold the stream value to search within (e.g., name or _values)

		// Determine effective stream values based on case sensitivity
		var effectiveStreamGroup = rawStreamGroup
		var effectiveStreamValues = rawStreamValues

		// Apply case insensitivity if needed
		if !filter.CaseSensitive {
			if streamGroupOK {
				if !lowerStreamGroupInit {
					lowerStreamGroup = strings.ToLower(rawStreamGroup)
					lowerStreamGroupInit = true
				}
				effectiveStreamGroup = lowerStreamGroup
			}
			if streamValuesOK {
				if !lowerStreamValuesInit {
					lowerStreamValues = strings.ToLower(rawStreamValues)
					lowerStreamValuesInit = true
				}
				effectiveStreamValues = lowerStreamValues
			}
		}

		// Perform the match based on filter type
		switch filter.Type {
		case "group-title":
			searchTarget = effectiveStreamGroup // For group-title, conditions check against stream group
			// Use precompiled rule
			if streamGroupOK && effectiveStreamGroup == filter.CompiledRule {
				match = true
				stream["_preserve-mapping"] = strconv.FormatBool(filter.PreserveMapping)
				stream["_starting-channel"] = filter.StartingChannel
			}
		case "custom-filter":
			searchTarget = effectiveStreamValues // For custom-filter, conditions check against stream values
			// Use precompiled rule
			if streamValuesOK && strings.Contains(effectiveStreamValues, filter.CompiledRule) {
				match = true
			}
		}

		if match {
			// If matched, check exclude/include conditions
			// `searchTarget` is already correctly cased. CompiledInclude/Exclude are also pre-cased if needed.
			if len(filter.CompiledExclude) > 0 {
				if !checkConditions(searchTarget, filter.PreparsedExclude, "exclude") {
					return false // Fails exclude condition
				}
			}
			if len(filter.CompiledInclude) > 0 {
				if !checkConditions(searchTarget, filter.PreparsedInclude, "include") {
					return false // Fails include condition
				}
			}
			return true // Matches filter and all its conditions
		}
	}
	return false // No filter matched
}

// Conditions for the Filter
func checkConditions(streamValues string, conditions []string, coType string) (status bool) {
	switch coType {
	case "exclude":
		status = true
	case "include":
		status = false
	}

	// Optimize: Use containsWholeWord to avoid allocating a padded string for every check
	// paddedStreamValues := " " + streamValues + " "

	// Key is pre-parsed in createFilterRules (unpadded)
	for _, key := range conditions {
		if containsWholeWord(streamValues, key) {
			switch coType {
			case "exclude":
				return false // Exclude if the exact phrase is found
			case "include":
				return true // Include if the exact phrase is found
			}
		}
	}

	return
}

// containsWholeWord checks if substr exists in s as a whole word (surrounded by spaces or start/end of string).
// This avoids allocating a new string with padding.
func containsWholeWord(s, substr string) bool {
	start := 0
	for {
		idx := strings.Index(s[start:], substr)
		if idx == -1 {
			return false
		}
		realIdx := start + idx

		isStartWord := realIdx == 0 || s[realIdx-1] == ' '

		end := realIdx + len(substr)
		isEndWord := end == len(s) || s[end] == ' '

		if isStartWord && isEndWord {
			return true
		}

		start = realIdx + 1
	}
}

// Create xTeVe M3U file
func buildM3U(groups []string) (m3u string, err error) {
	var imgc = Data.Cache.Images
	var m3uChannelsForSort []XEPGChannelStruct

	switch Settings.EpgSource {
	case "PMS":
		for i, dsa := range Data.Streams.Active {
			var stream, ok = dsa.(map[string]string)
			if !ok {
				continue
			}
			var channel XEPGChannelStruct

			channel.XName = stream["name"]
			channel.XGroupTitle = stream["group-title"]
			channel.TvgLogo = stream["tvg-logo"]
			channel.URL = stream["url"]
			channel.FileM3UID = stream["_file.m3u.id"]

			// Use tvg-id if present for the tvg-id attribute
			if tvgID, ok := stream["tvg-id"]; ok && len(tvgID) > 0 {
				channel.TvgID = tvgID
			}

			// Generate a numeric channel number for tvg-chno and for sorting
			channel.XChannelID = strconv.Itoa(i + 1000)
			channel.XEPG = channel.XChannelID // For channelID attribute

			if len(groups) > 0 {
				if !slices.Contains(groups, channel.XGroupTitle) {
					continue
				}
			}

			m3uChannelsForSort = append(m3uChannelsForSort, channel)
		}

	case "XEPG":
		for _, xepgChannel := range Data.XEPG.Channels {
			if xepgChannel.XActive {
				if len(groups) > 0 {
					if !slices.Contains(groups, xepgChannel.XGroupTitle) {
						continue // Not goto
					}
				}
				m3uChannelsForSort = append(m3uChannelsForSort, xepgChannel)
			}
		}
	}

	// Sort channels by numeric channel ID
	// Optimize: Pre-parse channel numbers to avoid repeated parsing during sort (O(n) vs O(n log n) parsing)
	type channelWithNum struct {
		channel *XEPGChannelStruct
		num     float64
	}

	tempChannels := make([]channelWithNum, 0, len(m3uChannelsForSort))
	for i := range m3uChannelsForSort {
		num, _ := strconv.ParseFloat(m3uChannelsForSort[i].XChannelID, 64)
		tempChannels = append(tempChannels, channelWithNum{
			channel: &m3uChannelsForSort[i],
			num:     num,
		})
	}

	slices.SortFunc(tempChannels, func(a, b channelWithNum) int {
		if a.num < b.num {
			return -1
		}
		if a.num > b.num {
			return 1
		}
		return 0
	})

	// Rebuild m3uChannelsForSort from sorted tempChannels
	m3uChannelsForSort = make([]XEPGChannelStruct, len(tempChannels))
	for i, tc := range tempChannels {
		m3uChannelsForSort[i] = *tc.channel
	}

	// Create M3U Content
	var xmltvURL = fmt.Sprintf("%s://%s/xmltv/xteve.xml", System.ServerProtocol.XML, System.Domain)
	// Use strings.Builder to optimize memory usage during concatenation
	var sb strings.Builder

	// Optimized M3U Header construction
	sb.WriteString(`#EXTM3U url-tvg="`)
	sb.WriteString(xmltvURL)
	sb.WriteString(`" x-tvg-url="`)
	sb.WriteString(xmltvURL)
	sb.WriteString("\"\n")

	for _, channel := range m3uChannelsForSort {
		// Use TvgID for tvg-id if it exists, otherwise fall back to XChannelID
		tvgID := channel.TvgID
		if len(tvgID) == 0 {
			tvgID = channel.XChannelID
		}

		// Optimized EXTINF line construction
		// Original: fmt.Fprintf(&sb, `#EXTINF:0 channelID="%s" tvg-chno="%s" tvg-name="%s" tvg-id="%s" tvg-logo="%s" group-title="%s",%s`+"\n", ...)
		sb.WriteString(`#EXTINF:0 channelID="`)
		sb.WriteString(channel.XEPG)
		sb.WriteString(`" tvg-chno="`)
		sb.WriteString(channel.XChannelID)
		sb.WriteString(`" tvg-name="`)
		sb.WriteString(channel.XName)
		sb.WriteString(`" tvg-id="`)
		sb.WriteString(tvgID)
		sb.WriteString(`" tvg-logo="`)
		sb.WriteString(imgc.Image.GetURL(channel.TvgLogo))
		sb.WriteString(`" group-title="`)
		sb.WriteString(channel.XGroupTitle)
		sb.WriteString(`",`)
		sb.WriteString(channel.XName)
		sb.WriteByte('\n')

		var stream, err = createStreamingURL("M3U", channel.FileM3UID, channel.XChannelID, channel.XName, channel.URL)
		if err == nil {
			sb.WriteString(stream)
			sb.WriteByte('\n')
		}
	}

	m3u = sb.String()

	if len(groups) == 0 {
		var filename = System.Folder.Data + "xteve.m3u"
		err = writeByteToFile(filename, []byte(m3u))
	}
	return
}
