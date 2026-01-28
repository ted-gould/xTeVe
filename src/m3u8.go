package src

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// getM3U8Bandwidth extracts bandwidth from #EXT-X-STREAM-INF line
func getM3U8Bandwidth(line string) int {
	if idx := strings.Index(line, "BANDWIDTH="); idx != -1 {
		var bandwidth = line[idx+10:]
		if comma := strings.Index(bandwidth, ","); comma != -1 {
			bandwidth = bandwidth[:comma]
		}
		n, err := strconv.Atoi(bandwidth)
		if err == nil {
			return n
		}
	}
	return 0
}

// parseM3U8Parameter parses M3U tags
func parseM3U8Parameter(line string, segment *Segment, stream *ThisStream, sequence *int64) error {
	line = strings.Trim(line, "\r\n")

	if strings.HasPrefix(line, "#EXTINF:") {
		var value = line[8:]
		if comma := strings.Index(value, ","); comma != -1 {
			value = value[:comma]
		}
		duration, err := strconv.ParseFloat(value, 64)
		if err == nil {
			segment.Duration = duration
		} else {
			ShowError(err, 1050)
			return err
		}
	} else if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
		segment.StreamInf.Bandwidth = getM3U8Bandwidth(line[18:])
	} else if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
		n, err := strconv.ParseInt(line[22:], 10, 64)
		if err == nil {
			stream.Sequence = n
			*sequence = n
		}
	} else if strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:") {
		segment.PlaylistType = line[21:]
	}

	return nil
}

// parseM3U8URL resolves the URL for a segment or playlist
func parseM3U8URL(line string, segment *Segment, stream *ThisStream) {
	// Optimization: Check prefixes to avoid expensive url.Parse calls.
	// Most lines in M3U8 are either absolute URLs, absolute paths, or relative paths.

	// 1. Absolute Path (starts with /)
	if strings.HasPrefix(line, "/") {
		segment.URL = stream.URLStreamingServer + line
		return
	}

	// 2. Full URL (http://, https://) or Protocol Relative (//)
	// Fast path for common schemes to avoid Contains check
	if strings.HasPrefix(line, "http") || strings.HasPrefix(line, "//") {
		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "//") {
			segment.URL = line
			return
		}
	}

	// Fallback for other schemes (rtmp://, etc)
	if strings.Contains(line, "://") {
		segment.URL = line
		return
	}

	// 3. Relative Path (fallback)
	// Optimization: Avoid path.Base and strings.Replace which allocate.
	if idx := strings.LastIndex(stream.M3U8URL, "/"); idx != -1 {
		segment.URL = stream.M3U8URL[:idx+1] + line
	} else {
		// Fallback if no slash found (unlikely for a valid URL context)
		segment.URL = line
	}
}

func ParseM3U8(stream *ThisStream) (err error) {
	var noNewSegment = false
	var lastSegmentDuration float64
	var segment Segment
	var m3u8Segments []Segment
	var sequence int64

	stream.DynamicBandwidth = false

	// Optimization: Avoid formatting debug string unless debug level is sufficient
	if System.Flag.Debug >= 3 {
		var debug = fmt.Sprintf(`M3U8 Playlist:`+"\n"+`%s`, stream.Body)
		showDebug(debug, 3)
	}

	if strings.Contains(stream.Body, "#EXTM3U") {
		if !stream.DynamicBandwidth {
			stream.DynamicStream = make(map[int]DynamicStream)
		}

		// Optimization: Use string slicing instead of bufio.Scanner to avoid allocation
		var remainder = stream.Body
		for len(remainder) > 0 {
			var line string
			if idx := strings.IndexByte(remainder, '\n'); idx >= 0 {
				line = remainder[:idx]
				remainder = remainder[idx+1:]
			} else {
				line = remainder
				remainder = ""
			}

			// Trim CR if present (Scanner does this automatically)
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}

			if len(line) > 0 {
				if line[0] == '#' {
					err := parseM3U8Parameter(line, &segment, stream, &sequence)
					if err != nil {
						return err
					}
					lastSegmentDuration = segment.Duration
				}

				// M3U8 contains several links to additional M3U8 Playlists (Bandwidth option)
				if segment.StreamInf.Bandwidth > 0 && line[0] != '#' {
					var dynamicStream DynamicStream

					segment.Duration = 0
					noNewSegment = false

					stream.DynamicBandwidth = true
					parseM3U8URL(line, &segment, stream)

					dynamicStream.Bandwidth = segment.StreamInf.Bandwidth
					dynamicStream.URL = segment.URL

					stream.DynamicStream[dynamicStream.Bandwidth] = dynamicStream
				}

				// Segment with TS Stream
				if segment.Duration > 0 && line[0] != '#' {
					parseM3U8URL(line, &segment, stream)

					if len(segment.URL) > 0 {
						segment.Sequence = sequence
						m3u8Segments = append(m3u8Segments, segment)
						sequence++
					}
				}
			}
		}

	} else {
		err = errors.New(getErrMsg(4051))
		return
	}

	if len(m3u8Segments) > 0 {
		isVOD := strings.Contains(stream.Body, "#EXT-X-ENDLIST") || strings.Contains(stream.Body, "#EXT-X-PLAYLIST-TYPE:VOD")
		if !stream.Status && isVOD {
			stream.Segment = m3u8Segments
			return nil
		}

		noNewSegment = true

		if !stream.Status {
			if len(m3u8Segments) >= 2 && !strings.Contains(stream.Body, "#EXT-X-ENDLIST") {
				m3u8Segments = m3u8Segments[0 : len(m3u8Segments)-1]
			}
		}

		for _, s := range m3u8Segments {
			segment = s

			if !stream.Status {
				noNewSegment = false
				stream.LastSequence = segment.Sequence

				// Stream is of type VOD. The first segment of the M3U8 playlist must be used.
				if strings.ToUpper(segment.PlaylistType) == "VOD" {
					break
				}
			} else {
				if segment.Sequence > stream.LastSequence {
					stream.LastSequence = segment.Sequence
					noNewSegment = false
					break
				}
			}
		}
	}

	if !noNewSegment {
		if stream.DynamicBandwidth {
			err = switchBandwidth(stream) // Check and assign error
			if err != nil {
				return err // Propagate error
			}
		} else {
			stream.Segment = append(stream.Segment, segment)
		}
	}

	if noNewSegment {
		var sleep = lastSegmentDuration * 0.5

		for i := 0.0; i < sleep*1000; i = i + 100 {
			_ = i
			time.Sleep(time.Duration(100) * time.Millisecond)

			if _, err := bufferVFS.Stat(stream.Folder); fsIsNotExistErr(err) {
				break
			}
		}
	}
	return
}
