package m3u

import (
	"errors"
	"log"
	"net/url"
	"regexp"
	"strings"

	"slices"
)

var exceptForParameterRx = regexp.MustCompile(`[a-z-A-Z=]*(".*?")`)
var exceptForChannelNameRx = regexp.MustCompile(`,([^\n]*|,[^\r]*)`)
var extGrpRx = regexp.MustCompile(`#EXTGRP: *(.*)`)

// MakeInterfaceFromM3U :
func MakeInterfaceFromM3U(byteStream []byte) (allChannels []any, err error) {
	var content = string(byteStream)
	// channelName is now local to parseMetaData
	processedUUIDs := make(map[string]struct{}) // For optimized UUID check across all channels

	var parseMetaData = func(channelBlock string) (stream map[string]string) { // currentProcessedUUIDs param removed, uses captured processedUUIDs
		stream = make(map[string]string)
		var channelName string // Made local

		var linesIn = strings.Split(strings.Replace(channelBlock, "\r\n", "\n", -1), "\n")

		// Optimized line filtering
		var lines []string 
		for _, line := range linesIn {
			if len(line) > 0 && line[0] != '#' { // Simplified condition
				lines = append(lines, line)
			}
		}

		if len(lines) >= 2 {
			for _, line := range lines {
				_, errURL := url.ParseRequestURI(line) // Renamed err to errURL

				switch errURL { // Use errURL
				case nil:
					stream["url"] = strings.Trim(line, "\r\n")
				default:
					var value string
					// Parse all parameters
					var streamParameter = exceptForParameterRx.FindAllString(line, -1)

					for _, p := range streamParameter {
						line = strings.Replace(line, p, "", 1)

						p = strings.Replace(p, `"`, "", -1)
						var parameter = strings.SplitN(p, "=", 2)

						if len(parameter) == 2 {
							// Set TVG Key as lowercase
							switch strings.Contains(parameter[0], "tvg") {
							case true:
								stream[strings.ToLower(parameter[0])] = parameter[1]
							case false:
								stream[parameter[0]] = parameter[1]
							}

							// URL's are not passed to the filter function
							if !strings.Contains(parameter[1], "://") && len(parameter[1]) > 0 {
								value = value + parameter[1] + " "
							}
						}
					}

					// Parse channel names
					var name = exceptForChannelNameRx.FindAllString(line, 1)

					if len(name) > 0 {
						channelName = name[0]
						channelName = strings.Replace(channelName, `,`, "", 1)
						channelName = strings.TrimRight(channelName, "\r\n")
						channelName = strings.Trim(channelName, " ")
					}

					if len(channelName) == 0 {
						if v, ok := stream["tvg-name"]; ok {
							channelName = v
						}
					}

					channelName = strings.Trim(channelName, " ")

					// Channels without names are skipped
					if len(channelName) == 0 {
						return
					}

					stream["name"] = channelName
					value = value + channelName

					stream["_values"] = value
				}
			}
		}

		// Search for a unique ID in the stream (optimized with map, using captured processedUUIDs)
		for key, value := range stream {
			if !strings.Contains(strings.ToLower(key), "tvg-id") {
				if strings.Contains(strings.ToLower(key), "id") {
					if _, exists := processedUUIDs[value]; exists {
						log.Printf("Channel: %s - %s = %s (Duplicate UUID based on non-tvg-id field)", stream["name"], key, value)
						// If a duplicate is found for this key, the original logic implies
						// _uuid.key and _uuid.value are NOT set by this specific id field.
						// The 'break' ensures we don't look for other 'id' fields in this stream.
					} else {
						// This is a new unique value for an "id" field.
						processedUUIDs[value] = struct{}{} // Mark this value as seen.
						stream["_uuid.key"] = key
						stream["_uuid.value"] = value
					}
					// Whether it was a duplicate or a new unique ID,
					// we break after processing the first encountered "id" field (non "tvg-id").
					break
				}
			}
		}
		return
	}

	if strings.Contains(content, "#EXT-X-TARGETDURATION") || strings.Contains(content, "#EXT-X-MEDIA-SEQUENCE") {
		err = errors.New("Invalid M3U file, an extended M3U file is required.")
		return
	}

	if strings.Contains(content, "#EXTM3U") {
		var channelBlocks = strings.Split(content, "#EXTINF") // Renamed 'channels' to 'channelBlocks'

		channelBlocks = slices.Delete(channelBlocks, 0, 1) // Remove the part before the first #EXTINF

		var lastExtGrp string

		for _, cb := range channelBlocks { // Iterate over channelBlocks
			// parseMetaData now uses the captured processedUUIDs from MakeInterfaceFromM3U
			var stream = parseMetaData(cb) 

			if extGrp := extGrpRx.FindStringSubmatch(cb); len(extGrp) > 1 {
				// EXTGRP applies to all subseqent channels until overriden
				lastExtGrp = strings.Trim(extGrp[1], "\r\n")
			}

			// group-title has priority over EXTGRP
			if stream["group-title"] == "" && lastExtGrp != "" {
				stream["group-title"] = lastExtGrp
			}

			if len(stream) > 0 && stream != nil {
				allChannels = append(allChannels, stream)
			}
		}
	} else {
		err = errors.New("Invalid M3U file, an extended M3U file is required.")
	}
	return
}
