package src

// Tests modelling the disconnect/reconnect behaviour observed in Comedy Central
// HD live stream recordings (stream 325781.ts) on the xteve_us_traces Axiom dataset.
//
// Observed trace patterns:
//
//   Normal session (2026-04-21, trace a05c8a674d58b26f788313c0ad881d68):
//     - One connectToStreamingServer span lasting 1h9m
//     - One processStreamingServerResponse span lasting the full session
//     - One long-lived HTTP GET (1h9m) feeding a single handleTSStream
//
//   Problematic sessions (e.g. 2026-04-15, trace a322ae8295686c453e795f4e848d0f32):
//     - One connectToStreamingServer span lasting ~9 minutes
//     - Many ~6-9 s processStreamingServerResponse cycles within that span
//     - Each cycle: fast HTTP GET (~200 ms redirect) + short HTTP GET (TS chunk) + handleTSStream
//     - The CDN closes the connection after every ~6-9 s segment, forcing repeated reconnects
//     - Each reconnect re-fetches from the live-stream buffer head, which is ~30 s behind
//       the live edge, producing a backward skip in the recorded content.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xteve/src/mpegts"
)

// TestConnectToStreamingServer_LiveDisconnectReconnectCycles models the short
// connection cycle seen in Comedy Central HD recordings.  The mock CDN closes
// every connection after a small burst of MPEG-TS data.  With retry enabled,
// xTeVe must reconnect automatically and continue buffering.
func TestConnectToStreamingServer_LiveDisconnectReconnectCycles(t *testing.T) {
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	const (
		// Minimum number of short CDN connections to observe before the test
		// terminates the client – matches the many reconnect cycles in the traces.
		wantConnections = 5

		// Packets served per connection.  Each 188-byte MPEG-TS packet is small;
		// real CDN segments are ~6-9 s of video at several Mbit/s, but for test
		// speed we keep the payload tiny.
		packetsPerConn = 8

		// Number of packets the mock CDN "steps back" at each reconnect.
		// In production recordings this translates to the ~30-second backward skip
		// the user observes: the live-stream buffer always starts from ~30 s behind
		// the live edge, so the new connection overlaps the previous one.
		overlapPackets = 2
	)

	var (
		connCount int64 // accessed via sync/atomic

		mu                sync.Mutex
		connStartPosition []int // absolute packet number at start of each connection
		nextLivePosition  = 0  // simulated live buffer head (server side)
	)

	// Each request handler represents one CDN connection: it writes packetsPerConn
	// MPEG-TS packets then returns, which closes the connection and causes an EOF on
	// the xTeVe side.  The absolute packet numbers encode which connection the data
	// came from so overlap can be verified later.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connNum := int(atomic.AddInt64(&connCount, 1))

		mu.Lock()
		startPos := nextLivePosition
		// Advance the live position by less than packetsPerConn to create overlap.
		nextLivePosition += packetsPerConn - overlapPackets
		connStartPosition = append(connStartPosition, startPos)
		mu.Unlock()

		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)

		for i := 0; i < packetsPerConn; i++ {
			absPos := startPos + i
			pkt := make([]byte, mpegts.PacketSize)
			pkt[0] = mpegts.SyncByte
			pkt[1] = byte(connNum)       // which reconnect cycle produced this packet
			pkt[2] = byte(absPos >> 8)   // absolute position high byte
			pkt[3] = byte(absPos & 0xFF) // absolute position low byte
			if _, err := w.Write(pkt); err != nil {
				return
			}
		}
		// Handler returns → connection closes → EOF on xTeVe client.
	}))
	defer server.Close()

	initBufferVFS(true)

	origRetry := Settings.StreamRetryEnabled
	origMax := Settings.StreamMaxRetries
	origDelay := Settings.StreamRetryDelay
	origBuf := Settings.BufferSize
	defer func() {
		Settings.StreamRetryEnabled = origRetry
		Settings.StreamMaxRetries = origMax
		Settings.StreamRetryDelay = origDelay
		Settings.BufferSize = origBuf
	}()

	// Use a 1 KB segment size so files roll over quickly and we can count them.
	Settings.BufferSize = 1
	Settings.UserAgent = "xTeVe-Test"
	Settings.StreamRetryEnabled = true
	Settings.StreamMaxRetries = wantConnections * 3
	Settings.StreamRetryDelay = 0 // reconnect immediately, as observed in traces

	playlistID := "M-comedy-central-reconnect"
	streamID := 0
	streamURL := server.URL
	channelName := "US: COMEDY CENTRAL HD"
	tempFolder := "/tmp/xteve_test_cc_reconnect/"

	md5Val, err := getMD5(streamURL)
	if err != nil {
		t.Fatalf("getMD5 failed: %v", err)
	}
	streamFolder := tempFolder + md5Val + string(os.PathSeparator)

	playlist := Playlist{
		Folder:       tempFolder,
		PlaylistID:   playlistID,
		PlaylistName: "US TV Stations",
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

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		connectToStreamingServer(streamID, playlistID, t.Context())
	}()

	// Wait until the mock CDN has served at least wantConnections requests.
	deadline := time.After(15 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out: only %d/%d CDN connections received",
				atomic.LoadInt64(&connCount), wantConnections)
		default:
		}
		if atomic.LoadInt64(&connCount) >= int64(wantConnections) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Simulate the DVR/client disconnecting, which is how the real session ends.
	killClientConnection(streamID, playlistID, false)

	// Wait for the streaming goroutine to finish before the deferred settings
	// restore runs, preventing a data race on the Settings struct.
	select {
	case <-serverDone:
	case <-time.After(5 * time.Second):
		t.Error("timed out waiting for connectToStreamingServer to exit after client kill")
	}

	// --- Assertions ---

	// 1. The server must have been hit at least wantConnections times.
	if got := atomic.LoadInt64(&connCount); got < int64(wantConnections) {
		t.Errorf("expected >= %d CDN connections (reconnect cycles), got %d",
			wantConnections, got)
	}

	// 2. Segment files must have been written across the reconnect cycles.
	files, err := bufferVFS.ReadDir(streamFolder)
	if err != nil {
		t.Fatalf("failed to read stream folder %s: %v", streamFolder, err)
	}
	if len(files) == 0 {
		t.Fatal("no segment files created: buffering produced no output across reconnect cycles")
	}

	// 3. Each reconnect must start overlapPackets before the previous connection
	//    ended, confirming the backward-skip pattern.
	mu.Lock()
	positions := make([]int, len(connStartPosition))
	copy(positions, connStartPosition)
	mu.Unlock()

	for i := 1; i < len(positions); i++ {
		prevEnd := positions[i-1] + packetsPerConn
		thisStart := positions[i]
		actualOverlap := prevEnd - thisStart
		if actualOverlap != overlapPackets {
			t.Errorf("reconnect %d: expected %d-packet backward skip (simulating 30 s overlap), got %d (prevEnd=%d, thisStart=%d)",
				i+1, overlapPackets, actualOverlap, prevEnd, thisStart)
		}
	}
}

// TestConnectToStreamingServer_LongLivedVsShortLived contrasts the normal
// long-lived connection (Apr 21 recording) against the problematic short-lived
// connection cycle (Apr 14-17 recordings).  Both scenarios must produce segment
// files; the short-lived scenario must generate more CDN connections.
func TestConnectToStreamingServer_LongLivedVsShortLived(t *testing.T) {
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	// Total TS packets to buffer in each scenario.
	const totalPackets = 40
	const packetsPerShortConn = 8 // short-lived CDN closes after 8 packets

	makePackets := func(total int) []byte {
		data := make([]byte, total*mpegts.PacketSize)
		for i := 0; i < total; i++ {
			off := i * mpegts.PacketSize
			data[off] = mpegts.SyncByte
			data[off+1] = byte(i >> 8)
			data[off+2] = byte(i & 0xFF)
		}
		return data
	}

	allPackets := makePackets(totalPackets)

	t.Run("LongLived", func(t *testing.T) {
		// Matches the Apr 21 trace: one HTTP connection streaming all data.
		var connCount int64

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt64(&connCount, 1)
			w.Header().Set("Content-Type", "video/mp2t")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(allPackets)
		}))
		defer server.Close()

		totalFiles := runBufferingScenario(t, server.URL, "long-lived", false, 0)

		if got := atomic.LoadInt64(&connCount); got != 1 {
			t.Errorf("long-lived scenario: expected 1 CDN connection, got %d", got)
		}
		if totalFiles == 0 {
			t.Error("long-lived scenario: no segment files written")
		}
	})

	t.Run("ShortLived", func(t *testing.T) {
		// Matches the Apr 14-17 traces: CDN closes after every packetsPerShortConn
		// packets, forcing xTeVe to reconnect repeatedly.
		var (
			connCount int64
			mu        sync.Mutex
			pos       = 0
		)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt64(&connCount, 1)

			mu.Lock()
			start := pos
			end := start + packetsPerShortConn
			if end > totalPackets {
				end = totalPackets
			}
			pos = end
			mu.Unlock()

			if start >= totalPackets {
				// All data exhausted; return 503 so xTeVe eventually stops retrying.
				http.Error(w, "no more data", http.StatusServiceUnavailable)
				return
			}

			w.Header().Set("Content-Type", "video/mp2t")
			w.WriteHeader(http.StatusOK)
			chunk := allPackets[start*mpegts.PacketSize : end*mpegts.PacketSize]
			_, _ = w.Write(chunk)
		}))
		defer server.Close()

		wantConns := (totalPackets + packetsPerShortConn - 1) / packetsPerShortConn

		totalFiles := runBufferingScenario(t, server.URL, "short-lived", true, wantConns)

		if got := atomic.LoadInt64(&connCount); got < int64(wantConns) {
			t.Errorf("short-lived scenario: expected >= %d CDN connections, got %d",
				wantConns, got)
		}
		if totalFiles == 0 {
			t.Error("short-lived scenario: no segment files written")
		}
	})
}

// runBufferingScenario is a helper that sets up a playlist, runs
// connectToStreamingServer, waits for stream completion, and returns the number
// of segment files created.  When retryEnabled is true and wantConns > 0 the
// helper allows the server to exhaust all data (server should return 503 once
// done), which causes xTeVe to stop retrying and finish naturally.
func runBufferingScenario(t *testing.T, streamURL, tag string, retryEnabled bool, wantConns int) (segmentFiles int) {
	t.Helper()

	initBufferVFS(true)

	origRetry := Settings.StreamRetryEnabled
	origMax := Settings.StreamMaxRetries
	origDelay := Settings.StreamRetryDelay
	origBuf := Settings.BufferSize
	defer func() {
		Settings.StreamRetryEnabled = origRetry
		Settings.StreamMaxRetries = origMax
		Settings.StreamRetryDelay = origDelay
		Settings.BufferSize = origBuf
	}()

	Settings.BufferSize = 1
	Settings.UserAgent = "xTeVe-Test"
	Settings.StreamRetryEnabled = retryEnabled
	Settings.StreamMaxRetries = wantConns * 3
	Settings.StreamRetryDelay = 0

	playlistID := "M-cc-" + tag
	streamID := 0
	channelName := "US: COMEDY CENTRAL HD"
	tempFolder := "/tmp/xteve_test_cc_" + tag + "/"

	md5Val, err := getMD5(streamURL)
	if err != nil {
		t.Fatalf("[%s] getMD5 failed: %v", tag, err)
	}
	streamFolder := tempFolder + md5Val + string(os.PathSeparator)

	playlist := Playlist{
		Folder:       tempFolder,
		PlaylistID:   playlistID,
		PlaylistName: "US TV Stations",
		Tuner:        1,
		Streams:      make(map[int]ThisStream),
		Clients:      make(map[int]ThisClient),
	}
	s := ThisStream{
		URL:         streamURL,
		ChannelName: channelName,
		Folder:      streamFolder,
		MD5:         md5Val,
		PlaylistID:  playlistID,
	}
	playlist.Streams[streamID] = s
	playlist.Clients[streamID] = ThisClient{Connection: 1}

	BufferInformation.Store(playlistID, &playlist)

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+md5Val, &clients)
	defer func() {
		BufferInformation.Delete(playlistID)
		BufferClients.Delete(playlistID + md5Val)
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		connectToStreamingServer(streamID, playlistID, t.Context())
	}()

	// Wait for the streaming goroutine to finish naturally (server exhausts data
	// and/or error terminates the loop), with a generous timeout.
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Errorf("[%s] timed out waiting for stream to finish", tag)
		killClientConnection(streamID, playlistID, false)
		<-done
	}

	files, readErr := bufferVFS.ReadDir(streamFolder)
	if readErr == nil {
		segmentFiles = len(files)
	}
	return segmentFiles
}
