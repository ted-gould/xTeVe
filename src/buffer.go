package src

/*
  Render Tuner Stream-Limit image as Video [ffmpeg]
  -loop 1 -i stream-limit.jpg -c:v libx264 -t 1 -pix_fmt yuv420p -vf scale=1920:1080  stream-limit.bin
*/

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

	"github.com/avfs/avfs/vfs/memfs"
	"github.com/avfs/avfs/vfs/osfs"
	"slices"
)

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

func bufferingStream(playlistID, streamingURL, channelName string, w http.ResponseWriter, r *http.Request) {
	time.Sleep(time.Duration(Settings.BufferTimeout) * time.Millisecond)

	var playlist Playlist
	var client ThisClient
	var stream ThisStream
	var streaming = false
	var streamID int
	var debug string
	var timeOut = 0
	var newStream = true

	//w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Connection", "close")

	// Check whether the Playlist is already in use
	if p, ok := BufferInformation.Load(playlistID); !ok {
		var playlistType string
		// Playlist is not yet used, create Default Values for the Playlist
		playlist.Folder = System.Folder.Temp + playlistID + string(os.PathSeparator)
		playlist.PlaylistID = playlistID
		playlist.Streams = make(map[int]ThisStream)
		playlist.Clients = make(map[int]ThisClient)

		err := checkVFSFolder(playlist.Folder, bufferVFS)
		if err != nil {
			ShowError(err, 000)
			httpStatusError(w, r, 404)
			return
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

		playlist.Streams[streamID] = stream
		playlist.Clients[streamID] = client

		BufferInformation.Store(playlistID, playlist)
	} else {
		// Playlist is already being used for streaming
		// Check if the URL is already being streamed by another Client

		playlist = p.(Playlist)

		for id := range playlist.Streams {
			stream = playlist.Streams[id]
			client = playlist.Clients[id]

			if streamingURL == stream.URL {
				streamID = id
				newStream = false
				client.Connection++

				//playlist.Streams[streamID] = stream
				playlist.Clients[streamID] = client

				BufferInformation.Store(playlistID, playlist)

				debug = fmt.Sprintf("Restream Status:Playlist: %s - Channel: %s - Connections: %d", playlist.PlaylistName, stream.ChannelName, client.Connection)

				showDebug(debug, 1)

				if c, ok := BufferClients.Load(playlistID + stream.MD5); ok {
					var clients = c.(ClientConnection)
					clients.Connection = clients.Connection + 1
					showInfo(fmt.Sprintf("Streaming Status:Channel: %s (Clients: %d)", stream.ChannelName, clients.Connection))

					BufferClients.Store(playlistID+stream.MD5, clients)
				}
				break
			}
		}

		// New Stream for an already active Playlist
		if newStream {
			// Check whether the Playlist allows another Stream (Tuner)
			if len(playlist.Streams) >= playlist.Tuner {
				showInfo(fmt.Sprintf("Streaming Status:Playlist: %s - No new connections available. Tuner = %d", playlist.PlaylistName, playlist.Tuner))

				content, err := webUI.ReadFile("video/stream-limit.bin")
				if err == nil {
					w.WriteHeader(200)
					w.Header().Set("Content-type", "video/mpeg")
					w.Header().Set("Content-Length:", "0")

					for i := 1; i < 60; i++ {
						_ = i
						if _, errWrite := w.Write(content); errWrite != nil {
							// Log error and break, client connection is likely gone
							// log.Printf("Error writing stream-limit content to client: %v", errWrite)
							return
						}
						time.Sleep(time.Duration(500) * time.Millisecond)
					}
					return
				}
				return
			}

			// Playlist allows another Stream (The Tuner limit has not yet been reached)
			// Create Default Values for the Stream
			stream = ThisStream{}
			client = ThisClient{}

			streamID = createStreamID(playlist.Streams)

			client.Connection = 1
			stream.URL = streamingURL
			stream.ChannelName = channelName
			stream.Status = false

			playlist.Streams[streamID] = stream
			playlist.Clients[streamID] = client

			BufferInformation.Store(playlistID, playlist)
		}
	}

	// Check whether the Stream is already being played by another Client
	if !playlist.Streams[streamID].Status && newStream {
		// New buffer is required
		stream = playlist.Streams[streamID]
		stream.MD5 = getMD5(streamingURL)
		stream.Folder = playlist.Folder + stream.MD5 + string(os.PathSeparator)
		stream.PlaylistID = playlistID
		stream.PlaylistName = playlist.PlaylistName

		playlist.Streams[streamID] = stream
		BufferInformation.Store(playlistID, playlist)

		switch Settings.Buffer {
		case "xteve":
			go connectToStreamingServer(streamID, playlistID)
		default:
			break
		}

		showInfo(fmt.Sprintf("Streaming Status:Playlist: %s - Tuner: %d / %d", playlist.PlaylistName, len(playlist.Streams), playlist.Tuner))

		var clients ClientConnection
		clients.Connection = 1
		BufferClients.Store(playlistID+stream.MD5, clients)
	}

	w.WriteHeader(200)

	for { // Loop 1: Wait until the first Segment has been downloaded by the Buffer
		if p, ok := BufferInformation.Load(playlistID); ok {
			var playlist = p.(Playlist)

			if stream, ok := playlist.Streams[streamID]; ok {
				if !stream.Status {
					timeOut++

					time.Sleep(time.Duration(100) * time.Millisecond)

					if c, ok := BufferClients.Load(playlistID + stream.MD5); ok {
						var clients = c.(ClientConnection)

						if clients.Error != nil || timeOut > 200 {
							killClientConnection(streamID, stream.PlaylistID, false)
							return
						}
					}
					continue
				}

				stream.OldSegments = []string{}

				for { // Loop 2: Temporary files are available, Data can be sent to the Client
					// Monitor HTTP Client connection
					ctx := r.Context()
					select {
					case <-ctx.Done():
						killClientConnection(streamID, playlistID, false)
						return
					default:
					}

					// Get the latest stream state from BufferInformation
					if p, ok := BufferInformation.Load(playlistID); ok {
						playlist := p.(Playlist)
						if s, ok := playlist.Streams[streamID]; ok {
							stream.StreamFinished = s.StreamFinished
						} else {
							// Stream has been removed, so we can exit
							return
						}
					} else {
						// Playlist has been removed, so we can exit
						return
					}

					if c, ok := BufferClients.Load(playlistID + stream.MD5); ok {
						var clients = c.(ClientConnection)
						if clients.Error != nil {
							ShowError(clients.Error, 0)
							killClientConnection(streamID, playlistID, false)
							return
						}
					} else {
						return
					}

					if _, err := bufferVFS.Stat(stream.Folder); fsIsNotExistErr(err) {
						killClientConnection(streamID, playlistID, false)
						return
					}

					Lock.Lock()
					var filesToSend []string
					if p, ok := BufferInformation.Load(playlistID); ok {
						playlist := p.(Playlist)
						if s, ok := playlist.Streams[streamID]; ok {
							filesToSend = make([]string, len(s.CompletedSegments))
							copy(filesToSend, s.CompletedSegments)
							s.CompletedSegments = []string{}
							playlist.Streams[streamID] = s
							BufferInformation.Store(playlistID, playlist)
						}
					}
					Lock.Unlock()

					for _, f := range filesToSend {
						var fileName = stream.Folder + f
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

							var buffer = make([]byte, int(l.Size()))
							_, err = file.Read(buffer)

							if err == nil {
								if !streaming {
									contentType := http.DetectContentType(buffer)
									w.Header().Set("Content-type", contentType)
									w.Header().Set("Content-Length", "0")
									w.Header().Set("Connection", "close")
								}

								if _, errWrite := w.Write(buffer); errWrite != nil {
									file.Close()
									killClientConnection(streamID, playlistID, false)
									return
								}

								streaming = true
							}
						}
						file.Close()

						stream.OldSegments = append(stream.OldSegments, f)

						// Clean up old segments
						if len(stream.OldSegments) > 20 {
							var fileToRemove = stream.Folder + stream.OldSegments[0]
							if err = bufferVFS.RemoveAll(getPlatformFile(fileToRemove)); err != nil {
								ShowError(err, 4007)
							}
							stream.OldSegments = slices.Delete(stream.OldSegments, 0, 1)
						}
					}

					if len(filesToSend) == 0 {
						if stream.StreamFinished {
							// No more files and stream is finished, so we can exit
							return
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
		var playlist = p.(Playlist)

		if force {
			delete(playlist.Streams, streamID)
			showInfo(fmt.Sprintf("Streaming Status:Playlist: %s - Tuner: %d / %d", playlist.PlaylistName, len(playlist.Streams), playlist.Tuner))
			return
		}

		if stream, ok := playlist.Streams[streamID]; ok {
			if c, ok := BufferClients.Load(playlistID + stream.MD5); ok {
				var clients = c.(ClientConnection)
				clients.Connection = clients.Connection - 1
				BufferClients.Store(playlistID+stream.MD5, clients)

				showInfo("Streaming Status:Client has terminated the connection")
				showInfo(fmt.Sprintf("Streaming Status:Channel: %s (Clients: %d)", stream.ChannelName, clients.Connection))

				if clients.Connection <= 0 {
					BufferClients.Delete(playlistID + stream.MD5)
					delete(playlist.Streams, streamID)
					delete(playlist.Clients, streamID)
				}
			}

			BufferInformation.Store(playlistID, playlist)

			if len(playlist.Streams) > 0 {
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

		if p, ok := BufferInformation.Load(stream.PlaylistID); ok {
			showInfo(fmt.Sprintf("Streaming Status:Channel: %s - No client is using this channel anymore. Streaming Server connection has ended", stream.ChannelName))

			var playlist = p.(Playlist)

			showInfo(fmt.Sprintf("Streaming Status:Playlist: %s - Tuner: %d / %d", playlist.PlaylistName, len(playlist.Streams), playlist.Tuner))

			if len(playlist.Streams) <= 0 {
				BufferInformation.Delete(stream.PlaylistID)
			}
		}
		status = false
	}
	return
}

func connectWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error
	var retries = 0

	for {
		resp, err = client.Do(req)

		if err != nil {
			if resp != nil {
				debugResponse(resp)
			}
			if Settings.StreamRetryEnabled && retries < Settings.StreamMaxRetries {
				retries++
				showInfo(fmt.Sprintf("Stream Error (%s). Retry %d/%d in %d milliseconds.", err.Error(), retries, Settings.StreamMaxRetries, Settings.StreamRetryDelay))
				time.Sleep(time.Duration(Settings.StreamRetryDelay) * time.Millisecond)
				continue
			}
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			if Settings.StreamRetryEnabled && retries < Settings.StreamMaxRetries {
				retries++
				showInfo(fmt.Sprintf("Stream HTTP Status Error (%s). Retry %d/%d in %d milliseconds.", http.StatusText(resp.StatusCode), retries, Settings.StreamMaxRetries, Settings.StreamRetryDelay))
				time.Sleep(time.Duration(Settings.StreamRetryDelay) * time.Millisecond)
				continue
			}
			return resp, fmt.Errorf("bad status: %s", resp.Status)
		}

		return resp, nil
	}
}

func connectToStreamingServer(streamID int, playlistID string) {
	if p, ok := BufferInformation.Load(playlistID); ok {
		var playlist = p.(Playlist)

		var timeOut = 0
		var debug string
		var tmpSegment = 1
		var tmpFolder = playlist.Streams[streamID].Folder
		var m3u8Segments []string
		var bandwidth BandwidthCalculation
		var networkBandwidth = Settings.M3U8AdaptiveBandwidthMBPS * 1e+6
		// Size of the Buffer
		var bufferSize = Settings.BufferSize
		var buffer = make([]byte, 1024*bufferSize)

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
			stream.NetworkBandwidth = networkBandwidth

			playlist.Streams[streamID] = stream

			timeOut++
		}

		var addErrorToStream = func(err error) {
			var stream = playlist.Streams[streamID]

			if c, ok := BufferClients.Load(playlistID + stream.MD5); ok {
				var clients = c.(ClientConnection)
				clients.Error = err
				BufferClients.Store(playlistID+stream.MD5, clients)
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

		var stream ThisStream = playlist.Streams[streamID]

		if !stream.Status {
			if strings.Contains(stream.URL, ".m3u8") {
				showInfo("Streaming Type:" + "[HLS / M3U8]")
			} else {
				showInfo("Streaming Type:" + "[TS]")
			}
			showInfo("Streaming URL:" + stream.URL)
		}

		var s = 0

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

			resp, err := connectWithRetry(client, req)

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
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					ShowError(err, 0)
					addErrorToStream(err)
				}

				stream.Body = string(body)
				stream.HLS = true
				stream.M3U8URL = currentURL

				err = parseM3U8(&stream)
				if err != nil {
					ShowError(err, 4050)
					addErrorToStream(err)
				}

				if stream.HLS {
					client := &http.Client{}

					for _, segment := range stream.Segment {
						req, _ := http.NewRequest("GET", segment.URL, nil)
						req.Header.Set("User-Agent", Settings.UserAgent)
						req.Header.Set("Connection", "close")
						req.Header.Set("Accept", "*/*")
						debugRequest(req)

						resp, err := connectWithRetry(client, req)
						if err != nil {
							ShowError(err, 0)
							addErrorToStream(err)
							return
						}
						defer resp.Body.Close()

						body, err := io.ReadAll(resp.Body)
						if err != nil {
							ShowError(err, 0)
							addErrorToStream(err)
						}

						tmpFile := fmt.Sprintf("%s%d.ts", tmpFolder, tmpSegment)
						bufferFile, err := bufferVFS.Create(tmpFile)
						if err != nil {
							addErrorToStream(err)
							bufferFile.Close()
							resp.Body.Close()
							return
						}

						if _, err := bufferFile.Write(body); err != nil {
							ShowError(err, 0)
							addErrorToStream(err)
							resp.Body.Close()
							return
						}
						bufferFile.Close()
						tmpSegment++
					}
				}
			// Video Stream (TS)
			case "video/mpeg", "video/mp4", "video/mp2t", "video/m2ts", "application/octet-stream", "binary/octet-stream", "application/mp2t", "video/x-matroska":
				var fileSize int
				var bytesWritten int

				// Size of the Buffer
				buffer = make([]byte, 1024*bufferSize)
				var tmpFileSize = 1024 * bufferSize * 1

				debug = fmt.Sprintf("Buffer Size:%d KB [SERVER CONNECTION]", len(buffer)/1024)
				showDebug(debug, 3)

				debug = fmt.Sprintf("Buffer Size:%d KB [CLIENT CONNECTION]", tmpFileSize/1024)
				showDebug(debug, 3)

				var tmpFile = fmt.Sprintf("%s%d.ts", tmpFolder, tmpSegment)

				if !clientConnection(stream) {
					resp.Body.Close()
					return
				}

				bufferFile, err := bufferVFS.Create(tmpFile)
				if err != nil {
					addErrorToStream(err)
					bufferFile.Close()
					resp.Body.Close()
					return
				}

				defer resp.Body.Close()
				for {
					if fileSize == 0 {
						debug = fmt.Sprintf("Buffer Status:Buffering (%s)", tmpFile)
						showDebug(debug, 2)
					}

					n, err := resp.Body.Read(buffer)
					if n > 0 {
						bytesWritten = 0
						for bytesWritten < n {
							// Calculate how much to write in this chunk
							writeSize := n - bytesWritten
							if fileSize+writeSize > tmpFileSize {
								writeSize = tmpFileSize - fileSize
							}

							if _, err := bufferFile.Write(buffer[bytesWritten : bytesWritten+writeSize]); err != nil {
								ShowError(err, 0)
								addErrorToStream(err)
								bufferFile.Close()
								return
							}
							fileSize += writeSize
							bytesWritten += writeSize

							// If the file is full, create a new one
							if fileSize >= tmpFileSize {
								Lock.Lock()

								bandwidth.Stop = time.Now()
								bandwidth.Size += fileSize

								bandwidth.TimeDiff = bandwidth.Stop.Sub(bandwidth.Start).Seconds()

								networkBandwidth = int(float64(bandwidth.Size) / bandwidth.TimeDiff * 1000)

								stream.NetworkBandwidth = networkBandwidth

								debug = fmt.Sprintf("Buffer Status:Done (%s)", tmpFile)
								showDebug(debug, 2)

								bufferFile.Close()

								stream.Status = true
								stream.CompletedSegments = append(stream.CompletedSegments, path.Base(tmpFile))
								playlist.Streams[streamID] = stream
								BufferInformation.Store(playlistID, playlist)
								Lock.Unlock()

								tmpSegment++

								tmpFile = fmt.Sprintf("%s%d.ts", tmpFolder, tmpSegment)

								if !clientConnection(stream) {
									if err = bufferVFS.RemoveAll(stream.Folder); err != nil {
										ShowError(err, 4005)
									}
									return
								}

								bufferFile, err = bufferVFS.Create(tmpFile)
								if err != nil {
									addErrorToStream(err)
									return
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
								goto Redirect
							}
							ShowError(err, 0)
							addErrorToStream(err)
						}

						// Handle the final segment on EOF
						Lock.Lock()
						if fileSize > 0 {
							stream.CompletedSegments = append(stream.CompletedSegments, path.Base(tmpFile))
						}
						stream.Status = true
						stream.StreamFinished = true
						playlist.Streams[streamID] = stream
						BufferInformation.Store(playlistID, playlist)
						Lock.Unlock()

						bufferFile.Close()
						break
					}
					retries = 0

					if !clientConnection(stream) {
						bufferFile.Close()
						return
					}
				}
				//--
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

func parseM3U8(stream *ThisStream) (err error) {
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
		var infos = strings.Split(line, ",")

		for _, info := range infos {
			if strings.Contains(info, "BANDWIDTH=") {
				var bandwidth = strings.Replace(info, "BANDWIDTH=", "", -1)
				n, err := strconv.Atoi(bandwidth)
				if err == nil {
					return n
				}
			}
		}
		return 0
	}

	var parseParameter = func(line string, segment *Segment) (err error) {
		line = strings.Trim(line, "\r\n")

		var parameters = []string{"#EXT-X-VERSION:", "#EXT-X-PLAYLIST-TYPE:", "#EXT-X-MEDIA-SEQUENCE:", "#EXT-X-STREAM-INF:", "#EXTINF:"}

		for _, parameter := range parameters {
			if strings.Contains(line, parameter) {
				var value = strings.Replace(line, parameter, "", -1)

				switch parameter {
				case "#EXT-X-VERSION:":
					version, err := strconv.Atoi(value)
					if err == nil {
						segment.Version = version
					}
				case "#EXT-X-PLAYLIST-TYPE:":
					segment.PlaylistType = value
				case "#EXT-X-MEDIA-SEQUENCE:":
					n, err := strconv.ParseInt(value, 10, 64)
					if err == nil {
						stream.Sequence = n
						sequence = n
					}
				case "#EXT-X-STREAM-INF:":
					segment.Info = true
					segment.StreamInf.Bandwidth = getBandwidth(value)
				case "#EXTINF:":
					var d = strings.Split(value, ",")
					if len(d) > 0 {
						value = strings.Replace(d[0], ",", "", -1)
						duration, err := strconv.ParseFloat(value, 64)
						if err == nil {
							segment.Duration = duration
						} else {
							ShowError(err, 1050)
							return err
						}
					}
				}
			}
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
		var lines = strings.Split(strings.Replace(stream.Body, "\r\n", "\n", -1), "\n")

		if !stream.DynamicBandwidth {
			stream.DynamicStream = make(map[int]DynamicStream)
		}

		// Parse Parameters
		for i, line := range lines {
			_ = i

			if len(line) > 0 {
				if line[0:1] == "#" {
					err := parseParameter(line, &segment)
					if err != nil {
						return err
					}
					lastSegmentDuration = segment.Duration
				}

				// M3U8 contains several links to additional M3U8 Playlists (Bandwidth option)
				if segment.Info && len(line) > 0 && line[0:1] != "#" {
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
	} else {
		err = errors.New(getErrMsg(4051))
		return
	}

	if len(m3u8Segments) > 0 {
		noNewSegment = true

		if !stream.Status {
			if len(m3u8Segments) >= 2 {
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
