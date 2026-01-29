package m3u

import (
	"errors"
	"log"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

//go:generate bash -c "regexp2go -flags=212 -pkg=m3u -fn=MatchAttribute -pool=true \"([a-zA-Z0-9-._]+)=\\\"([^\\\"]*)\\\"\" > regexp2go_attribute.go"

var matchAttribute MatchAttribute
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
		var processedHeader bool

		// Optimized: Iterate over lines without full split
		// channelBlock contains lines for one channel.
		// We expect #EXTINF line and URL line.

		lines := strings.Split(channelBlock, "\n")

		for _, rawLine := range lines {
			line := strings.TrimSpace(rawLine)
			if len(line) > 0 && line[0] != '#' {
				// This is either the EXTINF param line (starting with :) or the URL line.

				var isURL bool
				if processedHeader {
					// We've already processed the header (or skipped it), so this must be a URL
					isURL = true
				} else if strings.HasPrefix(line, ":") {
					// Definitive header indicator
					isURL = false
				} else {
					// Ambiguous case: Could be a URL (if header is missing) or a malformed header.
					// Optimization: Simple check for standard URL schemes to avoid expensive parsing
					if strings.Contains(line, "://") {
						isURL = true
					} else {
						// Fallback to strict parsing for edge cases (e.g. magnet links, relative URLs that look like garbage)
						// Note: ParseRequestURI fails for relative paths like "stream/1.ts", so those fall through to header parsing in the old code.
						// However, treating them as URL here fixes that bug while maintaining safety.
						_, errURL := url.ParseRequestURI(line)
						isURL = (errURL == nil)
					}
				}

				if isURL {
					// It's a URL
					stream["url"] = line
					processedHeader = true
				} else {
					processedHeader = true
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

						offset := 0
						for offset < len(attrPart) {
							matches, pos, ok := matchAttribute.FindString(attrPart[offset:])
							if !ok {
								break
							}
							key, val := matches[1], matches[2]

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

							offset += pos + len(matches[0])
						}
					} else {
						// Fallback if no comma found (unlikely for valid EXTINF but possible)
						// Just parse attributes from whole line?
						offset := 0
						for offset < len(line) {
							matches, pos, ok := matchAttribute.FindString(line[offset:])
							if !ok {
								break
							}
							key, val := matches[1], matches[2]
							if strings.Contains(key, "tvg") {
								stream[strings.ToLower(key)] = val
							} else {
								stream[key] = val
							}
							if !strings.Contains(val, "://") && len(val) > 0 {
								value += val + " "
							}
							offset += pos + len(matches[0])
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
