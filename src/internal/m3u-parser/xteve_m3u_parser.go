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

// parseAttributes extracts key="value" pairs and the channel name from an EXTINF line.
// It replaces regex operations to reduce allocations and CPU usage.
func parseAttributes(line string, stream map[string]string) (channelName string, value string) {
	// Format: ... attributes ... ,Channel Name
	// attributes: key="value"

	// Find the last comma, which separates attributes from the channel name
	// Note: Attributes can contain commas inside quotes, so we need to be careful.
	// However, the original regex logic for channel name was: `,([^\n]*|,[^\r]*)`
	// And attributes were extracted and REMOVED from the line first.

	// Strategy:
	// 1. Iterate through the string looking for key="value".
	// 2. Extract them and skip them.
	// 3. Whatever remains (ignoring spaces), if it starts with comma, is the name?

	// Let's iterate from left to right.
	n := len(line)
	i := 0

	for i < n {
		// Find '=' which indicates a potential key=value pair
		eqIdx := strings.IndexByte(line[i:], '=')
		if eqIdx == -1 {
			break
		}
		eqIdx += i

		// Check if it's followed by a quote
		if eqIdx+1 < n && line[eqIdx+1] == '"' {
			// Found key="...
			// Backtrack to find the start of the key
			keyEnd := eqIdx
			keyStart := strings.LastIndexAny(line[i:keyEnd], " ,")
			if keyStart == -1 {
				keyStart = i // Start of current segment
			} else {
				keyStart += i + 1 // After the space or comma
			}

			key := line[keyStart:keyEnd]

			// Find closing quote
			quoteStart := eqIdx + 2
			quoteEnd := strings.IndexByte(line[quoteStart:], '"')
			if quoteEnd == -1 {
				// Malformed, stop parsing attributes
				break
			}
			quoteEnd += quoteStart

			val := line[quoteStart:quoteEnd]

			// Store attribute
			// Set TVG Key as lowercase
			if strings.Contains(key, "tvg") {
				stream[strings.ToLower(key)] = val
			} else {
				stream[key] = val
			}

			// URL's are not passed to the filter function
			if !strings.Contains(val, "://") && len(val) > 0 {
				value = value + val + " "
			}

			// Advance i to after the closing quote
			i = quoteEnd + 1
		} else {
			// Not a quoted attribute, skip past this '='
			i = eqIdx + 1
		}
	}

	// Now find the channel name.
	// The original logic: exceptForChannelNameRx = regexp.MustCompile(`,([^\n]*|,[^\r]*)`)
	// It basically looks for the first comma that is NOT inside an attribute (since attributes were removed).

	// Since we didn't modify 'line' in place, we need to find the comma that separates info from name.
	// Standard EXTINF: #EXTINF:duration attributes,Channel Name

	// Finding the comma is tricky if attributes were not removed and contained commas.
	// But our parser above skipped over quoted values.

	// Let's do a second pass or integrate?
	// Actually, simpler: Iterate carefully.

	// Re-implementation of the full line parsing:
	// 1. Skip duration (if present, handled by regex in caller, but here we scan from start?)
	// Actually, caller passes the line which might start with specific params.

	// Let's just use LastIndex for comma?
	// Risky if channel name has comma. M3U says the *first* comma after EXTINF properties.

	// Correct approach: Scan and skip quoted sections. The first comma found outside quotes is the separator.

	commaPos := -1
	inQuote := false
	for idx, r := range line {
		if r == '"' {
			inQuote = !inQuote
		} else if r == ',' && !inQuote {
			commaPos = idx
			break
		}
	}

	if commaPos != -1 {
		channelName = line[commaPos+1:]
		channelName = strings.TrimSpace(channelName)
	}

	if len(channelName) == 0 {
		if v, ok := stream["tvg-name"]; ok {
			channelName = v
		}
	}
	channelName = strings.TrimSpace(channelName)

	// Clean up value (remove trailing space)
	if len(value) > 0 && value[len(value)-1] == ' ' {
		// value is mostly used for search, trailing space doesn't hurt much but let's match original
	}

	return
}

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
					var cName, val string
					cName, val = parseAttributes(line, stream)

					if len(cName) > 0 {
						channelName = cName
					}
					value += val
				}
			}
		}

		if len(channelName) > 0 {
			stream["name"] = channelName
			value += channelName
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
