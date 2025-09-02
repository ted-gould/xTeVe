package src

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"slices"

	"github.com/avfs/avfs/vfs/memfs"
	"github.com/avfs/avfs/vfs/osfs"
	"xteve/src/mpegts"
)

var errTunerLimitReached = errors.New("tuner limit reached")

func createStreamID(stream map[int]ThisStream) (streamID int) {
	var debug string

	streamID = 0
	for i := 0; i <= len(stream); i++ {
		if _, ok := stream[i]; !ok {
			streamID = i
			break
		}
	}

	debug = fmt.Sprintf("Streaming Status:Stream ID = %d", streamID)
	showDebug(debug, 1)

	return
}

func reserveStreamSlot(playlistID, streamingURL, channelName string) (*Playlist, ThisStream, ThisClient, int, bool, error) {
	Lock.Lock()
	defer Lock.Unlock()

	var playlist *Playlist
	var client ThisClient
	var stream ThisStream
	var streamID int
	var newStream = true

	if p, ok := BufferInformation.Load(playlistID); !ok {
		var playlistType string
		// Playlist is not yet used, create Default Values for the Playlist
		playlist = &Playlist{}
		playlist.Folder = System.Folder.Temp + playlistID + string(os.PathSeparator)
		playlist.PlaylistID = playlistID
		playlist.Streams = make(map[int]ThisStream)
		playlist.Clients = make(map[int]ThisClient)

		err := checkVFSFolder(playlist.Folder, bufferVFS)
		if err != nil {
			return playlist, stream, client, -1, false, err
		}

		switch playlist.PlaylistID[0:1] {
		case "M":
			playlistType = "m3u"
		case "H":
			playlistType = "hdhr"
		}

		playlist.Tuner = getTuner(playlistID, playlistType)
		playlist.PlaylistName = getProviderParameter(playlist.PlaylistID, playlistType, "name")

		// Create Default Values for the Stream
		streamID = createStreamID(playlist.Streams)

		client.Connection = 1
		stream.URL = streamingURL
		stream.ChannelName = channelName
		stream.Status = false
		// Populated from updateStreamWithMetadata
		stream.MD5 = getMD5(streamingURL)
		stream.Folder = playlist.Folder + stream.MD5 + string(os.PathSeparator)
		stream.PlaylistID = playlistID
		stream.PlaylistName = playlist.PlaylistName

		playlist.Streams[streamID] = stream
		playlist.Clients[streamID] = client

		BufferInformation.Store(playlistID, playlist)
	} else {
		// Playlist is already being used for streaming
		if pl, ok := p.(*Playlist); ok {
			playlist = pl
		} else {
			return nil, stream, client, -1, false, errors.New("invalid playlist type in map")
		}

		for id := range playlist.Streams {
			stream = playlist.Streams[id]
			client = playlist.Clients[id]

			if streamingURL == stream.URL {
				streamID = id
				newStream = false
				client.Connection++

				playlist.Clients[streamID] = client

				if c, ok := BufferClients.Load(playlistID + stream.MD5); ok {
					if clients, ok := c.(*ClientConnection); ok {
						clients.Connection = clients.Connection + 1
						showInfo(fmt.Sprintf("Streaming Status:Channel: %s (Clients: %d)", stream.ChannelName, clients.Connection))
					}
				}
				break
			}
		}

		// New Stream for an already active Playlist
		if newStream {
			if len(playlist.Streams) >= playlist.Tuner {
				showInfo(fmt.Sprintf("Streaming Status:Playlist: %s - No new connections available. Tuner = %d", playlist.PlaylistName, playlist.Tuner))
				return playlist, stream, client, -1, false, errTunerLimitReached
			}

			// Playlist allows another Stream (The Tuner limit has not yet been reached)
			stream = ThisStream{}
			client = ThisClient{}
			streamID = createStreamID(playlist.Streams)

			client.Connection = 1
			stream.URL = streamingURL
			stream.ChannelName = channelName
			stream.Status = false
			// Populated from updateStreamWithMetadata
			stream.MD5 = getMD5(streamingURL)
			stream.Folder = playlist.Folder + stream.MD5 + string(os.PathSeparator)
			stream.PlaylistID = playlistID
			stream.PlaylistName = playlist.PlaylistName

			playlist.Streams[streamID] = stream
			playlist.Clients[streamID] = client
		}
	}

	return playlist, stream, client, streamID, newStream, nil
}

func bufferingStream(playlistID, streamingURL, channelName string, w http.ResponseWriter, r *http.Request) {
	time.Sleep(time.Duration(Settings.BufferTimeout) * time.Millisecond)

	var playlist *Playlist
	var stream ThisStream
	var streaming = false
	var streamID int
	var timeOut = 0
	var newStream bool
	var err error
	var debug string

	w.Header().Set("Connection", "close")

	playlist, stream, _, streamID, newStream, err = reserveStreamSlot(playlistID, streamingURL, channelName)
	if err != nil {
		if err == errTunerLimitReached {
			content, err := webUI.ReadFile("video/stream-limit.bin")
			if err == nil {
				w.WriteHeader(200)
				w.Header().Set("Content-type", "video/mpeg")
				w.Header().Set("Content-Length:", "0")

				for i := 1; i < 60; i++ {
					_ = i
					if _, errWrite := w.Write(content); errWrite != nil {
						// Log error and break, client connection is likely gone
						return
					}
					time.Sleep(time.Duration(500) * time.Millisecond)
				}
			}
		} else {
			ShowError(err, 000)
			httpStatusError(w, r, 404)
		}
		return
	}

	// Check whether the Stream is already being played by another Client
	if !playlist.Streams[streamID].Status && newStream {
		// New buffer is required.
		// The stream entry is now created atomically in reserveStreamSlot.
		var clients ClientConnection
		clients.Connection = 1
		BufferClients.Store(playlistID+stream.MD5, &clients)

		switch Settings.Buffer {
		case "xteve":
			go connectToStreamingServer(streamID, playlistID)
		default:
			break
		}

		showInfo(fmt.Sprintf("Streaming Status:Playlist: %s - Tuner: %d / %d", playlist.PlaylistName, len(playlist.Streams), playlist.Tuner))

	}

	w.WriteHeader(200)

	for { // Loop 1: Wait until the first Segment has been downloaded by the Buffer
		if p, ok := BufferInformation.Load(playlistID); ok {
			var ok bool
			if playlist, ok = p.(*Playlist); !ok {
				// Should not happen
				return
			}

			if stream, ok := playlist.Streams[streamID]; ok {
				if !stream.Status {
					timeOut++

					time.Sleep(time.Duration(100) * time.Millisecond)

					if c, ok := BufferClients.Load(playlistID + stream.MD5); ok {
						if clients, ok := c.(*ClientConnection); ok {
							if clients.Error != nil || timeOut > 200 {
								killClientConnection(streamID, stream.PlaylistID, false)
								return
							}
						}
					}
					continue
				}

				stream.OldSegments = []string{}
				sentSegments := make(map[string]bool)

				for { // Loop 2: Temporary files are available, Data can be sent to the Client
					// Monitor HTTP Client connection
					ctx := r.Context()
					select {
					case <-ctx.Done():
						killClientConnection(streamID, playlistID, false)
						return
					default:
					}

					// --- New logic for handling multiple clients ---

					// 1. Get a copy of the segments from the shared buffer, safely
					segmentsToProcess, isStreamFinished, streamRemoved := getSegmentsAndStatus(playlistID, streamID)
					if streamRemoved {
						return
					}

					if c, ok := BufferClients.Load(playlistID + stream.MD5); ok {
						if clients, ok := c.(*ClientConnection); ok {
							if clients.Error != nil {
								ShowError(clients.Error, 0)
								killClientConnection(streamID, playlistID, false)
								return
							}
						}
					} else {
						return
					}

					if _, err := bufferVFS.Stat(stream.Folder); fsIsNotExistErr(err) {
						killClientConnection(streamID, playlistID, false)
						return
					}

					// 2. Determine which segments to send to this client
					type segmentToSend struct {
						Filename string
						Index    int
					}
					var filesToSend []segmentToSend
					for i, segInfo := range segmentsToProcess {
						if _, alreadySent := sentSegments[segInfo.Filename]; !alreadySent {
							filesToSend = append(filesToSend, segmentToSend{Filename: segInfo.Filename, Index: i})
						}
					}

					// 3. Send the files and update state
					for _, fts := range filesToSend {
						fileName := stream.Folder + fts.Filename
						file, err := bufferVFS.Open(fileName)
						if err != nil {
							debug = fmt.Sprintf("Buffer Open (%s)", fileName)
							showDebug(debug, 2)
							return
						}

						l, err := file.Stat()
						if err == nil {
							debug = fmt.Sprintf("Buffer Status:Send to client (%s)", fileName)
							showDebug(debug, 2)
							buffer := make([]byte, int(l.Size()))
							_, err = file.Read(buffer)
							if err == nil {
								if !streaming {
									contentType := http.DetectContentType(buffer)
									w.Header().Set("Content-type", contentType)
									w.Header().Set("Content-Length", "0")
									w.Header().Set("Connection", "close")
									streaming = true
								}
								if _, errWrite := w.Write(buffer); errWrite != nil {
									file.Close()
									killClientConnection(streamID, playlistID, false)
									return
								}
							}
						}
						file.Close()

						// Mark as sent for this client
						sentSegments[fts.Filename] = true
						stream.OldSegments = append(stream.OldSegments, fts.Filename)

						// Update the shared SentCount
						updateSegmentSentCount(playlistID, streamID, fts.Index, fts.Filename)

						// Cleanup completed segments
						cleanupCompletedSegments(playlistID, streamID, stream.MD5)
					}

					// Clean up old segment files from disk
					if len(stream.OldSegments) > 20 {
						fileToRemove := stream.Folder + stream.OldSegments[0]
						if err := bufferVFS.RemoveAll(getPlatformFile(fileToRemove)); err != nil {
							ShowError(err, 4007)
						}
						stream.OldSegments = slices.Delete(stream.OldSegments, 0, 1)
					}

					// 4. Wait if there's nothing to do
					if len(filesToSend) == 0 {
						if isStreamFinished {
							killClientConnection(streamID, playlistID, false)
							return // No more files and stream is finished
						}
						time.Sleep(time.Duration(100) * time.Millisecond)
					}
				} // End of Loop 2
			} else {
				// Stream not available
				killClientConnection(streamID, stream.PlaylistID, false)
				showInfo(fmt.Sprintf("Streaming Status:Playlist: %s - Tuner: %d / %d", playlist.PlaylistName, len(playlist.Streams), playlist.Tuner))
				return
			}
		} // End of Buffer Information
	} // End of Loop 1
}

func killClientConnection(streamID int, playlistID string, force bool) {
	Lock.Lock()
	defer Lock.Unlock()

	if p, ok := BufferInformation.Load(playlistID); ok {
		var playlist *Playlist
		if pl, ok := p.(*Playlist); ok {
			playlist = pl
		} else {
			return
		}

		if force {
			delete(playlist.Streams, streamID)
			showInfo(fmt.Sprintf("Streaming Status:Playlist: %s - Tuner: %d / %d", playlist.PlaylistName, len(playlist.Streams), playlist.Tuner))
			return
		}

		if stream, ok := playlist.Streams[streamID]; ok {
			if c, ok := BufferClients.Load(playlistID + stream.MD5); ok {
				if clients, ok := c.(*ClientConnection); ok {
					clients.Connection--

					showInfo("Streaming Status:Client has terminated the connection")
					showInfo(fmt.Sprintf("Streaming Status:Channel: %s (Clients: %d)", stream.ChannelName, clients.Connection))

					if clients.Connection <= 0 {
						BufferClients.Delete(playlistID + stream.MD5)
						delete(playlist.Streams, streamID)
						delete(playlist.Clients, streamID)
					}
				}
			}

			if len(playlist.Streams) == 0 {
				BufferInformation.Delete(playlistID)
				showInfo(fmt.Sprintf("Streaming Status:Playlist: %s - Tuner: 0 / %d", playlist.PlaylistName, playlist.Tuner))
			} else {
				showInfo(fmt.Sprintf("Streaming Status:Playlist: %s - Tuner: %d / %d", playlist.PlaylistName, len(playlist.Streams), playlist.Tuner))
			}
		}
	}
}

func clientConnection(stream ThisStream) (status bool) {
	status = true
	Lock.Lock()
	defer Lock.Unlock()

	if _, ok := BufferClients.Load(stream.PlaylistID + stream.MD5); !ok {
		var debug = fmt.Sprintf("Streaming Status:Remove temporary files (%s)", stream.Folder)
		showDebug(debug, 1)

		status = false

		debug = fmt.Sprintf("Remove tmp folder:%s", stream.Folder)
		showDebug(debug, 1)

		if err := bufferVFS.RemoveAll(stream.Folder); err != nil {
			ShowError(err, 4005)
		}

		if _, ok := BufferInformation.Load(stream.PlaylistID); ok {
			showInfo(fmt.Sprintf("Streaming Status:Channel: %s - No client is using this channel anymore. Streaming Server connection has ended", stream.ChannelName))
		}
		status = false
	}
	return
}

func connectToStreamingServer(streamID int, playlistID string) {
	if p, ok := BufferInformation.Load(playlistID); ok {
		var playlist *Playlist
		if pl, ok := p.(*Playlist); ok {
			playlist = pl
		} else {
			return
		}

		var timeOut = 0
		var debug string
		var tmpSegment = 1
		var tmpFolder = playlist.Streams[streamID].Folder
		var m3u8Segments []string
		var bandwidth BandwidthCalculation
		// Size of the Buffer
		var bufferSize = Settings.BufferSize
		var buffer = make([]byte, 1024*bufferSize)
		var stream ThisStream
		var s int

		var defaultSegment = func() {
			var segment Segment

			if len(playlist.Streams[streamID].Location) > 0 {
				segment.URL = playlist.Streams[streamID].Location
			} else {
				segment.URL = playlist.Streams[streamID].URL
			}

			segment.Duration = 0

			var stream = playlist.Streams[streamID]
			stream.Segment = []Segment{}
			stream.Segment = append(stream.Segment, segment)

			stream.HLS = false
			stream.Sequence = 0
			stream.Wait = 0
			stream.NetworkBandwidth = Settings.M3U8AdaptiveBandwidthMBPS * 1e+6

			playlist.Streams[streamID] = stream

			timeOut++
		}

		var addErrorToStream = func(err error) {
			if stream, ok := playlist.Streams[streamID]; ok {
				if c, ok := BufferClients.Load(playlistID + stream.MD5); ok {
					if clients, ok := c.(*ClientConnection); ok {
						clients.Error = err
					}
				}
			}
		}

		if err := bufferVFS.RemoveAll(getPlatformPath(tmpFolder)); err != nil {
			ShowError(err, 4005)
		}

		err := checkVFSFolder(tmpFolder, bufferVFS)
		if err != nil {
			ShowError(err, 0)
			addErrorToStream(err)
			return
		}

		// M3U8 Segments
	InitBuffer:
		defaultSegment()

		if len(m3u8Segments) > 30 {
			m3u8Segments = m3u8Segments[15:]
		}
		if timeOut >= 10 {
			return
		}

		stream = playlist.Streams[streamID]

		if !stream.Status {
			if strings.Contains(stream.URL, ".m3u8") {
				showInfo("Streaming Type:" + "[HLS / M3U8]")
			} else {
				showInfo("Streaming Type:" + "[TS]")
			}
			showInfo("Streaming URL:" + stream.URL)
		}

		s = 0

		stream.TimeStart = time.Now()
		bandwidth.Start = stream.TimeStart
		bandwidth.Size = 0

		for {
			if !clientConnection(stream) {
				return
			}

			if len(stream.Segment) == 0 || len(stream.URL) == 0 {
				goto InitBuffer
			}

			var segment = stream.Segment[0]
			var currentURL = strings.Trim(segment.URL, "\r\n")

			if len(currentURL) == 0 {
				goto InitBuffer
			}

			debug = fmt.Sprintf("Connection to:%s", currentURL)
			showDebug(debug, 2)

			var retries = 0
			// Jump for redirect (301 <---> 308)
		Redirect:
			req, _ := http.NewRequest("GET", currentURL, nil)
			req.Header.Set("User-Agent", Settings.UserAgent)
			req.Header.Set("Connection", "close")
			req.Header.Set("Accept", "*/*")
			debugRequest(req)

			client := &http.Client{}

			resp, err := ConnectWithRetry(client, req)

			if err != nil {
				ShowError(err, 0)
				addErrorToStream(err)
				if resp != nil {
					resp.Body.Close()
				}
				return
			}

			// Check HTTP Status, in case of errors the stream is terminated
			var contentType = resp.Header.Get("Content-Type")

			// Read out information about the streaming server
			if !stream.Status {
				if len(stream.URLStreamingServer) == 0 {
					u, _ := url.Parse(currentURL)
					p, _ := url.Parse(currentURL)

					stream.URLScheme = u.Scheme
					stream.URLHost = u.Host
					stream.URLPath = p.Path
					stream.URLFile = path.Base(p.Path)

					stream.URLRedirect = fmt.Sprintf("%s://%s%s", stream.URLScheme, stream.URLHost, stream.URLPath)
					stream.URLStreamingServer = fmt.Sprintf("%s://%s", stream.URLScheme, stream.URLHost)
				}

				debug = fmt.Sprintf("Server URL:%s", stream.URLStreamingServer)
				showDebug(debug, 1)

				debug = fmt.Sprintf("Temp Folder:%s", tmpFolder)
				showDebug(debug, 1)

				showInfo("Streaming Status:" + "HTTP Response Status [" + strconv.Itoa(resp.StatusCode) + "] " + http.StatusText(resp.StatusCode))
				showInfo("Content Type:" + contentType)
			} else {
				debug = fmt.Sprintf("Content Type:%s", contentType)
				showDebug(debug, 2)
			}

			// Clean up Content Type
			if len(contentType) > 0 {
				var ct = strings.SplitN(contentType, ";", 2)
				contentType = strings.ToLower(ct[0])
			}

			switch contentType {
			// M3U8 Playlist
			case "application/x-mpegurl", "application/vnd.apple.mpegurl", "audio/mpegurl", "audio/x-mpegurl":
				var err error
				stream, err = handleHLSStream(resp, stream, tmpFolder, &tmpSegment, addErrorToStream, currentURL)
				if err != nil {
					// handleHLSStream logs and adds errors, so we just need to return
					return
				}
			// Video Stream (TS)
			case "video/mpeg", "video/mp4", "video/mp2t", "video/m2ts", "application/octet-stream", "binary/octet-stream", "application/mp2t", "video/x-matroska":
				var err error
				stream, err = handleTSStream(resp, stream, streamID, playlistID, tmpFolder, &tmpSegment, addErrorToStream, buffer, &bandwidth, retries)
				if err != nil {
					if err.Error() == "redirect" {
						goto Redirect
					}
					addErrorToStream(err)
					return
				}
				// After handleTSStream returns, we need to get a fresh copy of the playlist
				// to avoid overwriting changes made by other goroutines (e.g. killClientConnection).
				if p, ok := BufferInformation.Load(playlistID); ok {
					if freshPlaylist, ok := p.(*Playlist); ok {
						// Only update the stream if it hasn't been removed from the playlist.
						if _, streamExists := freshPlaylist.Streams[streamID]; streamExists {
							freshPlaylist.Streams[streamID] = stream
							BufferInformation.Store(playlistID, freshPlaylist)
						}
					}
				}

			// Unknown Format
			default:
				showInfo("Content Type:" + resp.Header.Get("Content-Type"))
				err = errors.New("streaming error")
				ShowError(err, 4003)

				addErrorToStream(err)
				resp.Body.Close()
				return
			}

			s++

			if stream.StreamFinished && !stream.HLS {
				return
			}

			// Calculate the waiting time for the Download of the next Segment
			if stream.HLS {
				var sleep float64

				if segment.Duration > 0 {
					stream.TimeEnd = time.Now()
					stream.TimeDiff = stream.TimeEnd.Sub(stream.TimeStart).Seconds()

					sleep = max((segment.Duration-stream.TimeDiff)-(segment.Duration*0.25), 0)

					debug = fmt.Sprintf("HLS Status:Download time: %f s | Segment duration: %f s | Sleep: %f s Sequence: %d", stream.TimeDiff, segment.Duration, sleep, segment.Sequence)
					showDebug(debug, 1)

					if sleep > 0 {
						for i := 0.0; i < sleep*1000; i = i + 100 {
							_ = i
							time.Sleep(time.Duration(100) * time.Millisecond)

							if _, err := bufferVFS.Stat(stream.Folder); fsIsNotExistErr(err) {
								break
							}
						}
					}
				}
			}
			stream.Segment = stream.Segment[1:len(stream.Segment)]
			resp.Body.Close()
		} // End for loop
	} // End of BufferInformation
}

func handleHLSStream(resp *http.Response, stream ThisStream, tmpFolder string, tmpSegment *int, addErrorToStream func(err error), currentURL string) (ThisStream, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ShowError(err, 0)
		addErrorToStream(err)
		return stream, err
	}

	stream.Body = string(body)
	stream.HLS = true
	stream.M3U8URL = currentURL

	err = ParseM3U8(&stream)
	if err != nil {
		ShowError(err, 4050)
		addErrorToStream(err)
		return stream, err
	}

	if stream.HLS {
		client := &http.Client{}

		for _, segment := range stream.Segment {
			req, _ := http.NewRequest("GET", segment.URL, nil)
			req.Header.Set("User-Agent", Settings.UserAgent)
			req.Header.Set("Connection", "close")
			req.Header.Set("Accept", "*/*")
			debugRequest(req)

			segResp, err := ConnectWithRetry(client, req)
			if err != nil {
				ShowError(err, 0)
				addErrorToStream(err)
				return stream, err
			}

			body, err := io.ReadAll(segResp.Body)
			segResp.Body.Close() // Close body immediately after reading
			if err != nil {
				ShowError(err, 0)
				addErrorToStream(err)
				continue // Skip this segment
			}

			tmpFile := fmt.Sprintf("%s%d.ts", tmpFolder, *tmpSegment)
			bufferFile, err := bufferVFS.Create(tmpFile)
			if err != nil {
				addErrorToStream(err)
				bufferFile.Close()
				return stream, err
			}

			if _, err := bufferFile.Write(body); err != nil {
				ShowError(err, 0)
				addErrorToStream(err)
				bufferFile.Close()
				return stream, err
			}
			bufferFile.Close()
			*tmpSegment++
		}
	}

	return stream, nil
}

func handleTSStream(resp *http.Response, stream ThisStream, streamID int, playlistID, tmpFolder string, tmpSegment *int, addErrorToStream func(err error), buffer []byte, bandwidth *BandwidthCalculation, retries int) (ThisStream, error) {
	var fileSize int
	var bufferSize = Settings.BufferSize
	var tmpFileSize = 1024 * bufferSize * 1
	var debug string

	debug = fmt.Sprintf("Buffer Size:%d KB [SERVER CONNECTION]", len(buffer)/1024)
	showDebug(debug, 3)

	debug = fmt.Sprintf("Buffer Size:%d KB [CLIENT CONNECTION]", tmpFileSize/1024)
	showDebug(debug, 3)

	var tmpFile = fmt.Sprintf("%s%d.ts", tmpFolder, *tmpSegment)

	if !clientConnection(stream) {
		resp.Body.Close()
		return stream, nil
	}

	bufferFile, err := bufferVFS.Create(tmpFile)
	if err != nil {
		addErrorToStream(err)
		bufferFile.Close()
		resp.Body.Close()
		return stream, err
	}

	parser := mpegts.NewParser()

	defer resp.Body.Close()
	for {
		if fileSize == 0 {
			debug = fmt.Sprintf("Buffer Status:Buffering (%s)", tmpFile)
			showDebug(debug, 2)
		}

		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, err := parser.Write(buffer[:n]); err != nil {
				ShowError(err, 0)
				addErrorToStream(err)
				bufferFile.Close()
				return stream, err
			}
			for {
				packet, err := parser.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					ShowError(err, 0)
					addErrorToStream(err)
					bufferFile.Close()
					return stream, err
				}

				if _, err := bufferFile.Write(packet); err != nil {
					ShowError(err, 0)
					addErrorToStream(err)
					bufferFile.Close()
					return stream, err
				}
				fileSize += len(packet)

				if fileSize >= tmpFileSize {
					bufferFile.Close()
					completeTSsegment(playlistID, streamID, &stream, bandwidth, fileSize, tmpFile, *tmpSegment)
					*tmpSegment++

					tmpFile = fmt.Sprintf("%s%d.ts", tmpFolder, *tmpSegment)

					if !clientConnection(stream) {
						if err = bufferVFS.RemoveAll(stream.Folder); err != nil {
							ShowError(err, 4005)
						}
						return stream, nil
					}

					bufferFile, err = bufferVFS.Create(tmpFile)
					if err != nil {
						addErrorToStream(err)
						return stream, err
					}

					fileSize = 0
				}
			}
		}

		if err != nil {
			if err != io.EOF {
				if Settings.StreamRetryEnabled && retries < Settings.StreamMaxRetries {
					retries++
					showInfo(fmt.Sprintf("Stream Read Error (%s). Retry %d/%d in %d seconds.", err.Error(), retries, Settings.StreamMaxRetries, Settings.StreamRetryDelay))
					time.Sleep(time.Duration(Settings.StreamRetryDelay) * time.Second)
					bufferFile.Close()
					return stream, errors.New("redirect")
				}
				ShowError(err, 0)
				addErrorToStream(err)
			} else {
				// EOF reached, add the final segment if it has data
				if fileSize > 0 {
					segmentName := fmt.Sprintf("%d.ts", *tmpSegment)
					segmentInfo := SegmentInfo{Filename: segmentName, SentCount: 0}
					stream.CompletedSegments = append(stream.CompletedSegments, segmentInfo)

					// Update the stream in BufferInformation
					if p, ok := BufferInformation.Load(playlistID); ok {
						if playlist, ok := p.(*Playlist); ok {
							playlist.Streams[streamID] = stream
							BufferInformation.Store(playlistID, playlist)
						}
					}
				}
			}
			stream.Status = true
			stream.StreamFinished = true
			bufferFile.Close()
			break
		}
		retries = 0

		if !clientConnection(stream) {
			bufferFile.Close()
			return stream, nil
		}
	}
	return stream, nil
}

func switchBandwidth(stream *ThisStream) (err error) {
	var bandwidth []int
	var dynamicStream DynamicStream
	var segment Segment

	for key := range stream.DynamicStream {
		bandwidth = append(bandwidth, key)
	}

	sort.Ints(bandwidth)

	if len(bandwidth) > 0 {
		for i := range bandwidth {
			segment.StreamInf.Bandwidth = stream.DynamicStream[bandwidth[i]].Bandwidth

			dynamicStream = stream.DynamicStream[bandwidth[0]]

			if stream.NetworkBandwidth == 0 {
				dynamicStream = stream.DynamicStream[bandwidth[0]]
				break
			} else {
				if bandwidth[i] > stream.NetworkBandwidth {
					break
				}
				dynamicStream = stream.DynamicStream[bandwidth[i]]
			}
		}
	} else {
		err = errors.New("M3U8 does not contain streaming URLs")
		return
	}

	segment.URL = dynamicStream.URL
	segment.Duration = 0
	stream.Segment = append(stream.Segment, segment)

	return
}

// getSegmentsAndStatus safely retrieves the list of completed segments and the stream's finished status.
func getSegmentsAndStatus(playlistID string, streamID int) ([]SegmentInfo, bool, bool) {
	Lock.Lock()
	defer Lock.Unlock()

	p, ok := BufferInformation.Load(playlistID)
	if !ok {
		return nil, false, true // Playlist was removed
	}

	pl, ok := p.(*Playlist)
	if !ok {
		// This should not happen, indicates a type assertion error
		return nil, false, true
	}

	s, ok := pl.Streams[streamID]
	if !ok {
		return nil, false, true // Stream was removed
	}

	segmentsToProcess := make([]SegmentInfo, len(s.CompletedSegments))
	copy(segmentsToProcess, s.CompletedSegments)
	isStreamFinished := s.StreamFinished

	return segmentsToProcess, isStreamFinished, false
}

// updateSegmentSentCount safely increments the sent count of a segment.
func updateSegmentSentCount(playlistID string, streamID int, segmentIndex int, filename string) {
	Lock.Lock()
	defer Lock.Unlock()

	p, ok := BufferInformation.Load(playlistID)
	if !ok {
		return
	}

	pl, ok := p.(*Playlist)
	if !ok {
		return
	}

	if s, ok := pl.Streams[streamID]; ok {
		if segmentIndex < len(s.CompletedSegments) && s.CompletedSegments[segmentIndex].Filename == filename {
			s.CompletedSegments[segmentIndex].SentCount++
			pl.Streams[streamID] = s
			BufferInformation.Store(playlistID, pl)
		}
	}
}

// cleanupCompletedSegments safely removes segments that have been sent to all clients.
func cleanupCompletedSegments(playlistID string, streamID int, streamMD5 string) {
	Lock.Lock()
	defer Lock.Unlock()

	p, ok := BufferInformation.Load(playlistID)
	if !ok {
		return
	}

	pl, ok := p.(*Playlist)
	if !ok {
		return
	}

	if s, ok := pl.Streams[streamID]; ok {
		var numClients = 1
		if c, ok := BufferClients.Load(playlistID + streamMD5); ok {
			if clients, ok := c.(*ClientConnection); ok {
				numClients = clients.Connection
			}
		}

		var removeCount int
		for i, segInfo := range s.CompletedSegments {
			if segInfo.SentCount >= numClients {
				removeCount = i + 1
			} else {
				break
			}
		}

		if removeCount > 0 {
			s.CompletedSegments = s.CompletedSegments[removeCount:]
			pl.Streams[streamID] = s
			BufferInformation.Store(playlistID, pl)
		}
	}
}

func completeTSsegment(playlistID string, streamID int, stream *ThisStream, bandwidth *BandwidthCalculation, fileSize int, tmpFile string, tmpSegment int) {
	Lock.Lock()
	defer Lock.Unlock()

	bandwidth.Stop = time.Now()
	bandwidth.Size += fileSize
	bandwidth.TimeDiff = bandwidth.Stop.Sub(bandwidth.Start).Seconds()
	stream.NetworkBandwidth = int(float64(bandwidth.Size) / bandwidth.TimeDiff * 1000)

	debug := fmt.Sprintf("Buffer Status:Done (%s)", tmpFile)
	showDebug(debug, 2)

	// Add completed segment to the list
	segmentName := fmt.Sprintf("%d.ts", tmpSegment)
	segmentInfo := SegmentInfo{Filename: segmentName, SentCount: 0}
	stream.CompletedSegments = append(stream.CompletedSegments, segmentInfo)
	stream.Status = true

	if p, ok := BufferInformation.Load(playlistID); ok {
		if playlist, ok := p.(*Playlist); ok {
			playlist.Streams[streamID] = *stream
			BufferInformation.Store(playlistID, playlist)
		}
	}
}

func getTuner(id, playlistType string) (tuner int) {
	switch Settings.Buffer {
	case "-":
		tuner = Settings.Tuner
	case "xteve":
		i, err := strconv.Atoi(getProviderParameter(id, playlistType, "tuner"))
		if err == nil {
			tuner = i
		} else {
			ShowError(err, 0)
			tuner = 1
		}
	}
	return
}

func initBufferVFS(virtual bool) {
	if virtual {
		bufferVFS = memfs.New()
	} else {
		bufferVFS = osfs.New()
	}
}

func debugRequest(req *http.Request) {
	var debugLevel = 3

	if System.Flag.Debug < debugLevel {
		return
	}

	var debug string

	fmt.Println()
	debug = "Request:* * * * * * BEGIN HTTP(S) REQUEST * * * * * * "
	showDebug(debug, debugLevel)

	debug = fmt.Sprintf("Method:%s", req.Method)
	showDebug(debug, debugLevel)

	debug = fmt.Sprintf("Proto:%s", req.Proto)
	showDebug(debug, debugLevel)

	debug = fmt.Sprintf("URL:%s", req.URL)
	showDebug(debug, debugLevel)

	for name, headers := range req.Header {
		name = strings.ToLower(name)

		for _, h := range headers {
			debug = fmt.Sprintf("Header:%v: %v", name, h)
			showDebug(debug, debugLevel)
		}
	}

	debug = "Request:* * * * * * END HTTP(S) REQUEST * * * * * *"
	showDebug(debug, debugLevel)
}

func debugResponse(resp *http.Response) {
	var debugLevel = 3

	if System.Flag.Debug < debugLevel {
		return
	}

	var debug string

	fmt.Println()

	debug = "Response:* * * * * * BEGIN RESPONSE * * * * * * "
	showDebug(debug, debugLevel)

	debug = fmt.Sprintf("Proto:%s", resp.Proto)
	showDebug(debug, debugLevel)

	debug = fmt.Sprintf("Status Code:%d", resp.StatusCode)
	showDebug(debug, debugLevel)

	debug = fmt.Sprintf("Status Text:%s", http.StatusText(resp.StatusCode))
	showDebug(debug, debugLevel)

	for key, value := range resp.Header {
		switch fmt.Sprintf("%T", value) {
		case "[]string":
			debug = fmt.Sprintf("Header:%v: %s", key, strings.Join(value, " "))
		default:
			debug = fmt.Sprintf("Header:%v: %v", key, value)
		}
		showDebug(debug, debugLevel)
	}

	debug = "Pesponse:* * * * * * END RESPONSE * * * * * * "
	showDebug(debug, debugLevel)
}
