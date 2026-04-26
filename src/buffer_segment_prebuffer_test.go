package src

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"xteve/src/mpegts"
)

// writeTimestampTracker records the wall-clock time of every Write call so
// tests can detect pauses (stalls) in the data flow to the client.
type writeTimestampTracker struct {
	http.ResponseWriter
	mu    sync.Mutex
	times []time.Time
}

func (w *writeTimestampTracker) Write(b []byte) (int, error) {
	w.mu.Lock()
	w.times = append(w.times, time.Now())
	w.mu.Unlock()
	return w.ResponseWriter.Write(b)
}

func (w *writeTimestampTracker) writeTimes() []time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]time.Time, len(w.times))
	copy(out, w.times)
	return out
}

// segmentCountingWriter wraps an http.ResponseWriter and records the number
// of segment files present in the VFS at the moment the first byte is written
// to the client. This lets tests assert that the pre-buffer hold was respected
// before data delivery began.
type segmentCountingWriter struct {
	http.ResponseWriter
	once                   sync.Once
	firstWriteSegmentCount int
	streamFolder           string
}

func (w *segmentCountingWriter) Write(b []byte) (int, error) {
	w.once.Do(func() {
		files, _ := bufferVFS.ReadDir(w.streamFolder)
		w.firstWriteSegmentCount = len(files)
	})
	return w.ResponseWriter.Write(b)
}

// TestBufferingStream_WaitsForBufferSegments verifies that bufferingStream
// withholds data from the client until at least Settings.BufferSegments
// segments are present in the VFS. This is the pre-buffer that prevents player
// stutter when the upstream IPTV server has variable segment fetch latency.
func TestBufferingStream_WaitsForBufferSegments(t *testing.T) {
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	// 50 packets × 188 B = 9 400 B. With BufferSize=1 (1 KB per segment) this
	// produces ~9 segments, well above the BufferSegments=3 threshold.
	const numPackets = 50
	content := make([]byte, numPackets*mpegts.PacketSize)
	for i := 0; i < numPackets; i++ {
		copy(content[i*mpegts.PacketSize:], makePacketWithPCR(i))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}))
	defer server.Close()

	initBufferVFS(true)

	origSegments := Settings.BufferSegments
	origBufSize := Settings.BufferSize
	origTimeout := Settings.BufferTimeout
	origClientTimeout := Settings.BufferClientTimeout
	origBuffer := Settings.Buffer
	origRetry := Settings.StreamRetryEnabled
	origUA := Settings.UserAgent
	defer func() {
		Settings.BufferSegments = origSegments
		Settings.BufferSize = origBufSize
		Settings.BufferTimeout = origTimeout
		Settings.BufferClientTimeout = origClientTimeout
		Settings.Buffer = origBuffer
		Settings.StreamRetryEnabled = origRetry
		Settings.UserAgent = origUA
	}()

	Settings.BufferSegments = 3
	Settings.BufferSize = 1 // 1 KB per segment → ~9 segments from 50 packets
	Settings.BufferTimeout = 0
	Settings.BufferClientTimeout = 0
	Settings.Buffer = "xteve"
	Settings.StreamRetryEnabled = false
	Settings.UserAgent = "xTeVe-Test"

	playlistID := "M-buf-segments-test"
	streamID := 0
	streamURL := server.URL
	channelName := "US: ESPN"
	tempFolder := "/tmp/xteve_test_bufseg/"

	md5Val, err := getMD5(streamURL)
	if err != nil {
		t.Fatalf("getMD5: %v", err)
	}
	streamFolder := tempFolder + md5Val + string(os.PathSeparator)

	// Pre-populate state the same way TestBufferingStream_ClosesOnStreamEnd does:
	// bufferingStream will call reserveStreamSlot which finds the existing entry
	// and returns newStream=false, so the goroutine below is the only downloader.
	playlist := Playlist{
		Folder:       tempFolder,
		PlaylistID:   playlistID,
		PlaylistName: "TestPlaylist",
		Tuner:        1,
		Streams:      make(map[int]ThisStream),
		Clients:      make(map[int]ThisClient),
	}
	stream := ThisStream{
		URL:         streamURL,
		ChannelName: channelName,
		Status:      false,
		Folder:      streamFolder,
		MD5:         md5Val,
		PlaylistID:  playlistID,
	}
	playlist.Streams[streamID] = stream
	playlist.Clients[streamID] = ThisClient{Connection: 1}

	BufferInformation.Store(playlistID, &playlist)

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+md5Val, &clients)

	defer func() {
		BufferInformation.Delete(playlistID)
		BufferClients.Delete(playlistID + md5Val)
	}()

	go connectToStreamingServer(streamID, playlistID, t.Context())

	req := httptest.NewRequest("GET", "/stream", nil)
	tracker := &segmentCountingWriter{
		ResponseWriter: httptest.NewRecorder(),
		streamFolder:   streamFolder,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		bufferingStream(playlistID, streamURL, channelName, tracker, req)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("bufferingStream timed out")
	}

	if tracker.firstWriteSegmentCount == 0 {
		t.Fatal("no data was sent to the client")
	}
	if tracker.firstWriteSegmentCount < Settings.BufferSegments {
		t.Errorf("client received first byte with only %d segment(s) buffered, want >= %d (Settings.BufferSegments)",
			tracker.firstWriteSegmentCount, Settings.BufferSegments)
	}
}

// TestBufferingStream_ClientWaitsForSlowSource verifies that after the initial
// pre-buffer has been delivered, the client stalls and waits when the upstream
// source is slower than the client is consuming — i.e. the serving loop
// correctly blocks rather than terminating when there are temporarily no new
// segments available mid-stream.
func TestBufferingStream_ClientWaitsForSlowSource(t *testing.T) {
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	const (
		// 30 packets × 188 B = 5 640 B. With BufferSize=1 (1 KB/segment) this
		// produces ~5 complete segments per batch (segment boundary falls every
		// 6 packets: 6×188 = 1128 ≥ 1024).
		packetsPerBatch = 30
		// sourcePause is how long the server waits between the two batches.
		// The client must stall for approximately this long waiting for more data.
		sourcePause = 400 * time.Millisecond
	)

	buildBatch := func(startIdx int) []byte {
		data := make([]byte, packetsPerBatch*mpegts.PacketSize)
		for i := 0; i < packetsPerBatch; i++ {
			copy(data[i*mpegts.PacketSize:], makePacketWithPCR(startIdx+i))
		}
		return data
	}
	firstBatch := buildBatch(0)
	secondBatch := buildBatch(packetsPerBatch)

	// Server: send first batch → pause → send second batch → close.
	// The pause forces xTeVe's read loop to block, draining the client-side
	// segment queue before new segments become available.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(firstBatch)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(sourcePause)
		_, _ = w.Write(secondBatch)
	}))
	defer server.Close()

	initBufferVFS(true)

	origSegments := Settings.BufferSegments
	origBufSize := Settings.BufferSize
	origTimeout := Settings.BufferTimeout
	origClientTimeout := Settings.BufferClientTimeout
	origBuffer := Settings.Buffer
	origRetry := Settings.StreamRetryEnabled
	origUA := Settings.UserAgent
	defer func() {
		Settings.BufferSegments = origSegments
		Settings.BufferSize = origBufSize
		Settings.BufferTimeout = origTimeout
		Settings.BufferClientTimeout = origClientTimeout
		Settings.Buffer = origBuffer
		Settings.StreamRetryEnabled = origRetry
		Settings.UserAgent = origUA
	}()

	Settings.BufferSegments = 3
	Settings.BufferSize = 1
	Settings.BufferTimeout = 0
	Settings.BufferClientTimeout = 0
	Settings.Buffer = "xteve"
	Settings.StreamRetryEnabled = false
	Settings.UserAgent = "xTeVe-Test"

	playlistID := "M-slow-source-test"
	streamID := 0
	streamURL := server.URL
	channelName := "US: ESPN"
	tempFolder := "/tmp/xteve_test_slow/"

	md5Val, err := getMD5(streamURL)
	if err != nil {
		t.Fatalf("getMD5: %v", err)
	}
	streamFolder := tempFolder + md5Val + string(os.PathSeparator)

	playlist := Playlist{
		Folder:       tempFolder,
		PlaylistID:   playlistID,
		PlaylistName: "TestPlaylist",
		Tuner:        1,
		Streams:      make(map[int]ThisStream),
		Clients:      make(map[int]ThisClient),
	}
	stream := ThisStream{
		URL:         streamURL,
		ChannelName: channelName,
		Status:      false,
		Folder:      streamFolder,
		MD5:         md5Val,
		PlaylistID:  playlistID,
	}
	playlist.Streams[streamID] = stream
	playlist.Clients[streamID] = ThisClient{Connection: 1}

	BufferInformation.Store(playlistID, &playlist)
	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+md5Val, &clients)

	defer func() {
		BufferInformation.Delete(playlistID)
		BufferClients.Delete(playlistID + md5Val)
	}()

	go connectToStreamingServer(streamID, playlistID, t.Context())

	req := httptest.NewRequest("GET", "/stream", nil)
	tracker := &writeTimestampTracker{ResponseWriter: httptest.NewRecorder()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		bufferingStream(playlistID, streamURL, channelName, tracker, req)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("bufferingStream timed out")
	}

	times := tracker.writeTimes()
	if len(times) < 2 {
		t.Fatalf("expected writes from both batches, got only %d write(s)", len(times))
	}

	// Find the largest gap between consecutive writes. After the client has
	// consumed all segments from the first batch it must stall waiting for the
	// source to resume — that stall appears as a gap of roughly sourcePause in
	// the write timestamps.
	var maxGap time.Duration
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap > maxGap {
			maxGap = gap
		}
	}

	minExpectedGap := sourcePause / 2
	if maxGap < minExpectedGap {
		t.Errorf("client never stalled waiting for source: largest gap between writes was %v, want >= %v",
			maxGap, minExpectedGap)
	}
}
