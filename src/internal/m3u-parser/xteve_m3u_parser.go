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
var attributeRx = regexp.MustCompile(`([a-zA-Z0-9-._]+)="([^"]*)"`)

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
						matches := attributeRx.FindAllStringSubmatch(attrPart, -1)
						for _, m := range matches {
							key, val := m[1], m[2]

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
						}
					} else {
						// Fallback if no comma found (unlikely for valid EXTINF but possible)
						// Just parse attributes from whole line?
						matches := attributeRx.FindAllStringSubmatch(line, -1)
						for _, m := range matches {
							key, val := m[1], m[2]
							if strings.Contains(key, "tvg") {
								stream[strings.ToLower(key)] = val
							} else {
								stream[key] = val
							}
							if !strings.Contains(val, "://") && len(val) > 0 {
								value += val + " "
							}
						}
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
