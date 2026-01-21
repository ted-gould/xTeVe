package m3u

import (
	"errors"
	"log"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

var extGrpRx = regexp.MustCompile(`#EXTGRP: *(.*)`)
var durationRx = regexp.MustCompile(`^:(-?[0-9]+)`)
// MakeInterfaceFromM3U :
func MakeInterfaceFromM3U(byteStream []byte) (allChannels []any, err error) {
	var content = string(byteStream)
	// channelName is now local to parseMetaData
	processedUUIDs := make(map[string]struct{}) // For optimized UUID check across all channels

	// Using pointers to avoid map copying if possible, but the signature returns []any (likely []map[string]string)

	var parseMetaData = func(channelBlock string) (stream map[string]string) {
		stream = make(map[string]string)
		var channelName string
		var value string

		// Optimized: Iterate over lines without full split
		// channelBlock contains lines for one channel.
		// We expect #EXTINF line and URL line.

		lines := strings.Split(channelBlock, "\n")

		for _, rawLine := range lines {
			line := strings.TrimRight(rawLine, "\r")
			if len(line) > 0 && line[0] != '#' {
				// This is either the EXTINF param line (starting with :) or the URL line.

				_, errURL := url.ParseRequestURI(line)

				if errURL == nil {
					// It's a URL
					stream["url"] = line
				} else {
					// It's the parameter line (the part after #EXTINF)
					// Format: ... attributes ... ,Channel Name
					// Find separator comma (first comma not in quotes)
					commaPos := -1
					inQuote := false
					for i, r := range line {
						if r == '"' {
							inQuote = !inQuote
						} else if r == ',' && !inQuote {
							commaPos = i
							break
						}
					}

					if commaPos != -1 {
						channelName = strings.TrimSpace(line[commaPos+1:])

						// Parse attributes from the left part
						attrPart := line[:commaPos]
						parseAttributes(attrPart, func(key, val string) {
							// Set TVG Key as lowercase
							if strings.Contains(key, "tvg") {
								stream[strings.ToLower(key)] = val
							} else {
								stream[key] = val
							}

							// URL's are not passed to the filter function
							if !strings.Contains(val, "://") && len(val) > 0 {
								value += val + " "
							}
						})
					} else {
						// Fallback if no comma found (unlikely for valid EXTINF but possible)
						// Just parse attributes from whole line?
						parseAttributes(line, func(key, val string) {
							if strings.Contains(key, "tvg") {
								stream[strings.ToLower(key)] = val
							} else {
								stream[key] = val
							}
							if !strings.Contains(val, "://") && len(val) > 0 {
								value += val + " "
							}
						})
					}

					if len(channelName) == 0 {
						if v, ok := stream["tvg-name"]; ok {
							channelName = v
						}
					}
					channelName = strings.TrimSpace(channelName)

					value += channelName
				}
			}
		}

		if len(channelName) > 0 {
			stream["name"] = channelName
			stream["_values"] = value
		} else {
			// If no name found, skip
			return nil
		}

		if durationMatch := durationRx.FindStringSubmatch(channelBlock); len(durationMatch) > 1 {
			stream["_duration"] = durationMatch[1]
		}

		// Search for a unique ID in the stream (optimized with map, using captured processedUUIDs)
		for key, value := range stream {
			lowerKey := strings.ToLower(key)
			if !strings.Contains(lowerKey, "tvg-id") {
				if strings.Contains(lowerKey, "id") {
					if _, exists := processedUUIDs[value]; exists {
						log.Printf("Channel: %s - %s = %s (Duplicate UUID based on non-tvg-id field)", stream["name"], key, value)
					} else {
						processedUUIDs[value] = struct{}{}
						stream["_uuid.key"] = key
						stream["_uuid.value"] = value
					}
					break
				}
			}
		}
		return stream
	}

	if strings.Contains(content, "#EXT-X-TARGETDURATION") || strings.Contains(content, "#EXT-X-MEDIA-SEQUENCE") {
		err = errors.New("Invalid M3U file, an extended M3U file is required.")
		return
	}

	if strings.Contains(content, "#EXTM3U") {
		var channelBlocks = strings.Split(content, "#EXTINF")
		channelBlocks = slices.Delete(channelBlocks, 0, 1)

		var lastExtGrp string

		for _, cb := range channelBlocks {
			stream := parseMetaData(cb)

			if stream == nil {
				continue
			}

			if extGrp := extGrpRx.FindStringSubmatch(cb); len(extGrp) > 1 {
				lastExtGrp = strings.TrimSpace(extGrp[1])
			}

			if stream["group-title"] == "" && lastExtGrp != "" {
				stream["group-title"] = lastExtGrp
			}

			allChannels = append(allChannels, stream)
		}
	} else {
		err = errors.New("Invalid M3U file, an extended M3U file is required.")
	}
	return
}

// parseAttributes replaces the regex `([a-zA-Z0-9-._]+)="([^"]*)"`
// It iterates through the string finding key="value" pairs and calls the callback for each match.
// This eliminates repeated regex execution and submatch allocations.
func parseAttributes(line string, callback func(key, val string)) {
	n := len(line)
	i := 0
	for i < n {
		// Find next '='
		eqIdx := strings.IndexByte(line[i:], '=')
		if eqIdx == -1 {
			break
		}
		absEqIdx := i + eqIdx

		// Check if followed by quote
		if absEqIdx+1 >= n || line[absEqIdx+1] != '"' {
			i = absEqIdx + 1
			continue
		}

		// Backtrack to find key start
		keyEnd := absEqIdx
		keyStart := keyEnd
		for keyStart > i {
			c := line[keyStart-1]
			if !isKeyChar(c) {
				break
			}
			keyStart--
		}

		if keyStart == keyEnd {
			// No valid key found
			i = absEqIdx + 1
			continue
		}

		key := line[keyStart:keyEnd]

		// Find end of value
		valStart := absEqIdx + 2
		closeQuoteIdx := strings.IndexByte(line[valStart:], '"')
		if closeQuoteIdx == -1 {
			break
		}
		valEnd := valStart + closeQuoteIdx
		val := line[valStart:valEnd]

		// Clone strings to avoid retaining reference to the potentially large source line/file
		callback(strings.Clone(key), strings.Clone(val))

		i = valEnd + 1
	}
}

func isKeyChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.' || c == '_'
}
