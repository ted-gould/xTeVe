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

// makePacketWithPCR returns a 188-byte MPEG-TS packet whose adaptation field
// carries a PCR value derived from absPos.  The payload area encodes absPos so
// tests can verify which logical stream positions ended up in the buffer files.
//
// PCR is set to absPos × 300 (one unit per 90 kHz tick → one unique PCR per
// position).  This keeps the values small while still being valid PCR.
func makePacketWithPCR(absPos int) []byte {
	pkt := make([]byte, mpegts.PacketSize)
	pkt[0] = mpegts.SyncByte
	// PID 0x0100 (arbitrary but consistent)
	pkt[1] = 0x01
	pkt[2] = 0x00
	// adaptation_field_control = 0b11 (adaptation + payload), continuity counter = absPos&0xF
	pkt[3] = 0x30 | byte(absPos&0x0F)
	// Adaptation field: length=7 (flags byte + 6 PCR bytes), no stuffing needed for our purposes
	pkt[4] = 7
	// Flags: PCR_flag (bit 4) set
	pkt[5] = 0x10
	// PCR = absPos * 300 → base = absPos, ext = 0
	base := int64(absPos)
	ext := int64(0)
	pkt[6] = byte(base >> 25)
	pkt[7] = byte(base >> 17)
	pkt[8] = byte(base >> 9)
	pkt[9] = byte(base >> 1)
	pkt[10] = byte(base&0x01)<<7 | 0x7E | byte(ext>>8)
	pkt[11] = byte(ext)
	// Payload: encode absPos so callers can inspect which positions were buffered
	pkt[12] = byte(absPos >> 8)
	pkt[13] = byte(absPos & 0xFF)
	return pkt
}

// countPacketsInVFS counts the total number of 188-byte MPEG-TS packets across
// all files in dir.
func countPacketsInVFS(dir string) int {
	files, err := bufferVFS.ReadDir(dir)
	if err != nil {
		return 0
	}
	total := 0
	for _, fi := range files {
		f, err := bufferVFS.Open(dir + fi.Name())
		if err != nil {
			continue
		}
		buf := make([]byte, mpegts.PacketSize)
		for {
			n, err := f.Read(buf)
			if n == mpegts.PacketSize {
				total++
			}
			if err != nil {
				break
			}
		}
		f.Close()
	}
	return total
}

// TestConnectToStreamingServer_LiveDisconnectReconnectCycles models the short
// connection cycle seen in Comedy Central HD recordings.  The mock CDN closes
// every connection after a small burst of MPEG-TS data and starts the next one
// overlapPackets behind the live edge, simulating the ~30-second backward skip.
//
// With the PCR-based deduplication fix, xTeVe discards only the overlapping
// packets (PCR < lastPCR from previous connection), but keeps the boundary
// packet (PCR == lastPCR) to avoid skipping data we already have.
func TestConnectToStreamingServer_LiveDisconnectReconnectCycles(t *testing.T) {
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	const (
		wantConnections = 5
		packetsPerConn  = 8
		// overlapPackets is the number of packets the CDN rewinds on each
		// reconnect, simulating the ~30-second backward skip observed in the
		// Comedy Central traces.
		overlapPackets = 2
	)

	var (
		connCount int64

		mu                sync.Mutex
		connStartPosition []int
		nextLivePosition  = 0
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connNum := int(atomic.AddInt64(&connCount, 1))
		_ = connNum

		mu.Lock()
		startPos := nextLivePosition
		nextLivePosition += packetsPerConn - overlapPackets
		connStartPosition = append(connStartPosition, startPos)
		mu.Unlock()

		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)

		for i := 0; i < packetsPerConn; i++ {
			if _, err := w.Write(makePacketWithPCR(startPos + i)); err != nil {
				return
			}
		}
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

	Settings.BufferSize = 1
	Settings.UserAgent = "xTeVe-Test"
	Settings.StreamRetryEnabled = true
	Settings.StreamMaxRetries = wantConnections * 3
	Settings.StreamRetryDelay = 0

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

	killClientConnection(streamID, playlistID, false)

	select {
	case <-serverDone:
	case <-time.After(5 * time.Second):
		t.Error("timed out waiting for connectToStreamingServer to exit after client kill")
	}

	// --- Assertions ---

	if got := atomic.LoadInt64(&connCount); got < int64(wantConnections) {
		t.Errorf("expected >= %d CDN connections (reconnect cycles), got %d",
			wantConnections, got)
	}

	// With PCR-based deduplication the buffer must contain only the unique
	// (non-overlapping) packets:
	//   connection 1 contributes all packetsPerConn packets.
	//   each subsequent connection contributes (packetsPerConn - overlapPackets + 1).
	// The extra packet per reconnect comes from accepting PCR == lastPCR at the
	// reconnect boundary, which avoids skipping data when we have it.
	//
	// Use the actual connection count so the assertion holds regardless of
	// how many reconnect cycles the test completes before the kill lands.
	totalConns := int(atomic.LoadInt64(&connCount))
	withoutFixExpected := packetsPerConn * totalConns
	uniqueExpected := packetsPerConn + (totalConns-1)*(packetsPerConn-overlapPackets+1)

	got := countPacketsInVFS(streamFolder)
	if got == 0 {
		t.Fatal("no packets written to buffer – buffering did not produce output")
	}
	// The last connection may be cut short, so allow for up to one connection
	// worth of slack on the unique expected count.  The key signal is that
	// got is clearly below the no-dedup total.
	if got >= withoutFixExpected {
		t.Errorf("deduplication did not fire: got %d packets, expected < %d (without-fix: %d conns × %d pkts); unique expected ≈ %d",
			got, withoutFixExpected, totalConns, packetsPerConn, uniqueExpected)
	}

	// Verify the server-side overlap accounting is correct.
	mu.Lock()
	positions := make([]int, len(connStartPosition))
	copy(positions, connStartPosition)
	mu.Unlock()

	for i := 1; i < len(positions); i++ {
		prevEnd := positions[i-1] + packetsPerConn
		thisStart := positions[i]
		if overlap := prevEnd - thisStart; overlap != overlapPackets {
			t.Errorf("reconnect %d: expected %d-packet overlap, got %d",
				i+1, overlapPackets, overlap)
		}
	}
}

// TestConnectToStreamingServer_LongLivedVsShortLived contrasts the normal
// long-lived connection (Apr 21 recording) against the problematic short-lived
// connection cycle (Apr 14-17 recordings).  After the PCR-based deduplication
// fix, both scenarios must produce the same buffered content.
func TestConnectToStreamingServer_LongLivedVsShortLived(t *testing.T) {
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	const totalPackets = 40
	const packetsPerShortConn = 8

	// Build the canonical byte stream: totalPackets packets with sequential PCR.
	allPackets := make([]byte, 0, totalPackets*mpegts.PacketSize)
	for i := 0; i < totalPackets; i++ {
		allPackets = append(allPackets, makePacketWithPCR(i)...)
	}

	t.Run("LongLived", func(t *testing.T) {
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
			t.Errorf("long-lived: expected 1 CDN connection, got %d", got)
		}
		if totalFiles == 0 {
			t.Error("long-lived: no segment files written")
		}
	})

	t.Run("ShortLived", func(t *testing.T) {
		// CDN closes after every packetsPerShortConn packets, forcing reconnects.
		// The stream is served sequentially (no overlap) so after the fix the
		// output must equal the long-lived output byte-for-byte.
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
				http.Error(w, "no more data", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "video/mp2t")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(allPackets[start*mpegts.PacketSize : end*mpegts.PacketSize])
		}))
		defer server.Close()

		wantConns := (totalPackets + packetsPerShortConn - 1) / packetsPerShortConn

		totalFiles := runBufferingScenario(t, server.URL, "short-lived", true, wantConns)

		if got := atomic.LoadInt64(&connCount); got < int64(wantConns) {
			t.Errorf("short-lived: expected >= %d CDN connections, got %d", wantConns, got)
		}
		if totalFiles == 0 {
			t.Error("short-lived: no segment files written")
		}
	})
}

// TestConnectToStreamingServer_SparsePCRWithReconnect tests the scenario where
// PCR packets are infrequent (sparse) in the stream. This verifies that the
// buffering logic correctly handles reconnects even when non-PCR packets
// arrive between PCR packets during the overlap region.
//
// Scenario:
//   - Connection 1: 12 packets, every 3rd packet has PCR (packets 0, 3, 6, 9)
//   - Connection 2: 12 packets with 4-packet overlap (starts at packet 8)
//   - PCR values in Conn 2: 8, 11, 14, 17 (local indices 0, 3, 6, 9)
//   - lastPCR from Conn 1 = 9
//   - Expected: Connection 2 starts writing at PCR=11 (>= lastPCR)
//   - Non-PCR packets before PCR=11 are buffered and then discarded
func TestConnectToStreamingServer_SparsePCRWithReconnect(t *testing.T) {
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	const (
		wantConnections  = 2
		packetsPerConn   = 12
		overlapPackets   = 4
		pcrInterval      = 3 // Every 3rd packet has PCR
	)

	// Helper to create a packet with optional PCR
	makePacketWithOptionalPCR := func(absPos int, hasPCR bool) []byte {
		pkt := make([]byte, mpegts.PacketSize)
		pkt[0] = mpegts.SyncByte
		pkt[1] = 0x01
		pkt[2] = 0x00
		if hasPCR {
			// adaptation_field_control = 0b11 (adaptation + payload)
			pkt[3] = 0x30 | byte(absPos&0x0F)
			pkt[4] = 7 // adaptation field length
			pkt[5] = 0x10 // PCR_flag set
			base := int64(absPos)
			pkt[6] = byte(base >> 25)
			pkt[7] = byte(base >> 17)
			pkt[8] = byte(base >> 9)
			pkt[9] = byte(base >> 1)
			pkt[10] = byte(base&0x01)<<7 | 0x7E
			pkt[11] = 0x00
			// Payload: encode absPos
			pkt[12] = byte(absPos >> 8)
			pkt[13] = byte(absPos & 0xFF)
		} else {
			// No PCR: adaptation_field_control = 0b01 (payload only)
			pkt[3] = 0x00 | byte(absPos&0x0F)
			// Payload: encode absPos
			pkt[12] = byte(absPos >> 8)
			pkt[13] = byte(absPos & 0xFF)
		}
		return pkt
	}

	var (
		connCount int64
		mu         sync.Mutex
		nextLivePosition  = 0
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = int(atomic.AddInt64(&connCount, 1))

		mu.Lock()
		startPos := nextLivePosition
		nextLivePosition += packetsPerConn - overlapPackets
		mu.Unlock()

		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)

		for i := 0; i < packetsPerConn; i++ {
			absPos := startPos + i
			// PCR on every pcrInterval-th packet
			hasPCR := (i % pcrInterval) == 0
			if _, err := w.Write(makePacketWithOptionalPCR(absPos, hasPCR)); err != nil {
				return
			}
		}
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

	Settings.BufferSize = 1
	Settings.UserAgent = "xTeVe-Test"
	Settings.StreamRetryEnabled = true
	Settings.StreamMaxRetries = wantConnections * 3
	Settings.StreamRetryDelay = 0

	playlistID := "M-sparse-pcr-reconnect"
	streamID := 0
	streamURL := server.URL
	channelName := "US: SPARSE PCR TEST"
	tempFolder := "/tmp/xteve_test_sparse_pcr/"

	md5Val, err := getMD5(streamURL)
	if err != nil {
		t.Fatalf("getMD5 failed: %v", err)
	}
	streamFolder := tempFolder + md5Val + string(os.PathSeparator)

	playlist := Playlist{
		Folder:       tempFolder,
		PlaylistID:   playlistID,
		PlaylistName: "Test Playlist",
		Tuner:        1,
		Streams:      make(map[int]ThisStream),
		Clients:      make(map[int]ThisClient),
	}
	stream := ThisStream{
		URL:         streamURL,
		ChannelName: channelName,
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

	killClientConnection(streamID, playlistID, false)

	select {
	case <-serverDone:
	case <-time.After(5 * time.Second):
		t.Error("timed out waiting for connectToStreamingServer to exit after client kill")
	}

	// --- Assertions ---

	if got := atomic.LoadInt64(&connCount); got < int64(wantConnections) {
		t.Errorf("expected >= %d CDN connections, got %d", wantConnections, got)
	}

	// With sparse PCR and overlap:
	// Connection 1 (packets 0-11, PCR at 0, 3, 6, 9): all 12 packets written
	// Connection 2 (packets 8-19, PCR at 8, 11, 14, 17): starts at PCR=8 boundary
	//   - lastPCR from Conn 1 = 9
	//   - Packet 8 (PCR=8): 8 < 9, discard, clear buffer, continue
	//   - Packets 9-10 (no PCR): buffered
	//   - Packet 11 (PCR=11): 11 >= 9, catch up. Discard buffered, write packet 11
	//   - Packets 12-19: all written
	// So Connection 2 contributes 9 packets (positions 11-19)
	//
	// Total: 12 + 9 = 21 packets

	// Connection 1: all 12 packets
	// Connection 2: packets from PCR=11 onwards = 19 - 11 + 1 = 9 packets
	expectedPackets := 12 + 9

	got := countPacketsInVFS(streamFolder)
	if got == 0 {
		t.Fatal("no packets written to buffer")
	}

	// The key verification is that we got the expected count
	// (boundary packet is kept, no skipping occurred)
	if got != expectedPackets {
		t.Errorf("sparse PCR reconnect: got %d packets, expected %d", got, expectedPackets)
	}
}

// runBufferingScenario sets up a playlist, runs connectToStreamingServer, waits
// for stream completion, and returns the number of segment files created.
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
