package src

import (
	"fmt"
	"io"
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

	// We used to pad streamValues with spaces to perform whole-word matching via strings.Contains.
	// This was causing memory allocations in the hot loop.
	// Now we use containsWholeWord to check for the key with word boundaries without allocation.

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

// containsWholeWord checks if substr exists in s as a whole word.
// A whole word is defined as being surrounded by spaces or string boundaries.
// This function avoids allocating new strings.
func containsWholeWord(s, substr string) bool {
	if substr == "" {
		return false
	}

	start := 0
	for {
		idx := strings.Index(s[start:], substr)
		if idx == -1 {
			return false
		}
		idx += start // Adjust index to original string

		// Check boundaries
		// Start boundary: Start of string OR preceding character is a space
		isStartBound := idx == 0 || s[idx-1] == ' '
		// End boundary: End of string OR following character is a space
		isEndBound := idx+len(substr) == len(s) || s[idx+len(substr)] == ' '

		if isStartBound && isEndBound {
			return true
		}

		// Move start forward to continue searching
		// We can advance by 1.
		// Optimization: We could advance by idx + 1
		start = idx + 1
		if start >= len(s) {
			return false
		}
	}
}

// Create xTeVe M3U file
func buildM3U(groups []string) (m3u string, err error) {
	var sb strings.Builder
	err = buildM3UToWriter(&sb, groups)
	if err != nil {
		return "", err
	}
	m3u = sb.String()

	if len(groups) == 0 {
		var filename = System.Folder.Data + "xteve.m3u"
		err = writeByteToFile(filename, []byte(m3u))
	}
	return
}

// buildM3UToWriter writes M3U content to the provided io.Writer.
// This allows streaming output to avoid large memory allocations.
func buildM3UToWriter(w io.Writer, groups []string) (err error) {
	var imgc = Data.Cache.Images

	// Collect channels to sort
	type channelWithNum struct {
		channel XEPGChannelStruct
		num     float64
	}

	capacityEstimate := len(Data.XEPG.Channels)
	if Settings.EpgSource == "PMS" {
		capacityEstimate = len(Data.Streams.Active)
	}

	tempChannels := make([]channelWithNum, 0, capacityEstimate)

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

			num, _ := strconv.ParseFloat(channel.XChannelID, 64)
			tempChannels = append(tempChannels, channelWithNum{
				channel: channel,
				num:     num,
			})
		}

	case "XEPG":
		for _, xepgChannel := range Data.XEPG.Channels {
			if xepgChannel.XActive {
				if len(groups) > 0 {
					if !slices.Contains(groups, xepgChannel.XGroupTitle) {
						continue // Not goto
					}
				}

				num, _ := strconv.ParseFloat(xepgChannel.XChannelID, 64)
				tempChannels = append(tempChannels, channelWithNum{
					channel: xepgChannel,
					num:     num,
				})
			}
		}
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

	// Create M3U Content
	var xmltvURL = fmt.Sprintf("%s://%s/xmltv/xteve.xml", System.ServerProtocol.XML, System.Domain)

	// Optimized M3U Header construction
	// Helper to handle write errors
	write := func(s string) {
		if err != nil {
			return
		}
		_, err = io.WriteString(w, s)
	}

	write(`#EXTM3U url-tvg="`)
	write(xmltvURL)
	write(`" x-tvg-url="`)
	write(xmltvURL)
	write("\"\n")

	for _, tc := range tempChannels {
		if err != nil {
			return err
		}

		channel := tc.channel

		// Use TvgID for tvg-id if it exists, otherwise fall back to XChannelID
		tvgID := channel.TvgID
		if len(tvgID) == 0 {
			tvgID = channel.XChannelID
		}

		// Optimized EXTINF line construction
		write(`#EXTINF:0 channelID="`)
		write(channel.XEPG)
		write(`" tvg-chno="`)
		write(channel.XChannelID)
		write(`" tvg-name="`)
		write(channel.XName)
		write(`" tvg-id="`)
		write(tvgID)
		write(`" tvg-logo="`)
		write(imgc.Image.GetURL(channel.TvgLogo))
		write(`" group-title="`)
		write(channel.XGroupTitle)
		write(`",`)
		write(channel.XName)
		write("\n")

		var stream string
		var streamErr error
		stream, streamErr = createStreamingURL("M3U", channel.FileM3UID, channel.XChannelID, channel.XName, channel.URL)
		if streamErr == nil {
			write(stream)
			write("\n")
		}
	}

	return err
}
