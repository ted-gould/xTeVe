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
				effectiveStreamGroup = strings.ToLower(rawStreamGroup)
			}
			if streamValuesOK {
				effectiveStreamValues = strings.ToLower(rawStreamValues)
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
				if !checkConditions(searchTarget, filter.CompiledExclude, "exclude") {
					return false // Fails exclude condition
				}
			}
			if len(filter.CompiledInclude) > 0 {
				if !checkConditions(searchTarget, filter.CompiledInclude, "include") {
					return false // Fails include condition
				}
			}
			return true // Matches filter and all its conditions
		}
	}
	return false // No filter matched
}

// Conditions for the Filter
func checkConditions(streamValues, conditions, coType string) (status bool) {
	switch coType {
	case "exclude":
		status = true
	case "include":
		status = false
	}

	conditions = strings.Replace(conditions, ", ", ",", -1)
	conditions = strings.Replace(conditions, " ,", ",", -1)

	var keys = strings.Split(conditions, ",")

	// Pad streamValues to handle matches at the beginning or end of the string.
	// This ensures that we are matching whole words or phrases.
	paddedStreamValues := " " + streamValues + " "

	for _, key := range keys {
		if key == "" {
			continue
		}

		// Pad the key to ensure we match the exact phrase surrounded by spaces.
		paddedKey := " " + key + " "
		if strings.Contains(paddedStreamValues, paddedKey) {
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
		for _, dxc := range Data.XEPG.Channels {
			var xepgChannel XEPGChannelStruct
			err := bindToStruct(dxc, &xepgChannel)
			if err == nil {
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
	}

	// Sort channels by numeric channel ID
	slices.SortFunc(m3uChannelsForSort, func(a, b XEPGChannelStruct) int {
		numA, _ := strconv.ParseFloat(a.XChannelID, 64)
		numB, _ := strconv.ParseFloat(b.XChannelID, 64)
		if numA < numB {
			return -1
		}
		if numA > numB {
			return 1
		}
		return 0
	})

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
