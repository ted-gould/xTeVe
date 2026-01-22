package src

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

func ParseM3U8(stream *ThisStream) (err error) {
	var debug string
	var noNewSegment = false
	var lastSegmentDuration float64
	var segment Segment
	var m3u8Segments []Segment
	var sequence int64

	stream.DynamicBandwidth = false

	debug = fmt.Sprintf(`M3U8 Playlist:`+"\n"+`%s`, stream.Body)
	showDebug(debug, 3)

	var getBandwidth = func(line string) int {
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

	var parseParameter = func(line string, segment *Segment) (err error) {
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
			segment.StreamInf.Bandwidth = getBandwidth(line[18:])
		} else if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			n, err := strconv.ParseInt(line[22:], 10, 64)
			if err == nil {
				stream.Sequence = n
				sequence = n
			}
		} else if strings.HasPrefix(line, "#EXT-X-PLAYLIST-TYPE:") {
			segment.PlaylistType = line[21:]
		}

		return
	}

	var parseURL = func(line string, segment *Segment) {
		// Check if the address is a valid URL (http://... or /path/to/stream)
		_, err := url.ParseRequestURI(line)
		if err == nil {
			// Prüfen ob die Domain in der Adresse enhalten ist
			u, _ := url.Parse(line)

			if len(u.Host) == 0 {
				// Check whether the domain is included in the address
				segment.URL = stream.URLStreamingServer + line
			} else {
				// Domain included in the address
				segment.URL = line
			}
		} else {
			// not URL, but a file path (media/file-01.ts)
			var serverURLPath = strings.Replace(stream.M3U8URL, path.Base(stream.M3U8URL), line, -1)
			segment.URL = serverURLPath
		}
	}

	if strings.Contains(stream.Body, "#EXTM3U") {
		if !stream.DynamicBandwidth {
			stream.DynamicStream = make(map[int]DynamicStream)
		}

		scanner := bufio.NewScanner(strings.NewReader(stream.Body))

		// Parse Parameters
		for scanner.Scan() {
			line := scanner.Text()

			if len(line) > 0 {
				if line[0:1] == "#" {
					err := parseParameter(line, &segment)
					if err != nil {
						return err
					}
					lastSegmentDuration = segment.Duration
				}

				// M3U8 contains several links to additional M3U8 Playlists (Bandwidth option)
				if segment.StreamInf.Bandwidth > 0 && len(line) > 0 && line[0:1] != "#" {
					var dynamicStream DynamicStream

					segment.Duration = 0
					noNewSegment = false

					stream.DynamicBandwidth = true
					parseURL(line, &segment)

					dynamicStream.Bandwidth = segment.StreamInf.Bandwidth
					dynamicStream.URL = segment.URL

					stream.DynamicStream[dynamicStream.Bandwidth] = dynamicStream
				}

				// Segment with TS Stream
				if segment.Duration > 0 && line[0:1] != "#" {
					parseURL(line, &segment)

					if len(segment.URL) > 0 {
						segment.Sequence = sequence
						m3u8Segments = append(m3u8Segments, segment)
						sequence++
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			return err
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
