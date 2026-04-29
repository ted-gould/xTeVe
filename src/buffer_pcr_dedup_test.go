package src

// Tests for the PCR-based reconnect deduplication logic in
// processTSStreamPacketsVFS / handleTSStream.
//
// Each test calls handleTSStream directly — bypassing the HTTP layer — so the
// packet sequence for each "connection" is fully controlled.  After all
// connections, the positions written to the VFS segment files are read back
// and compared against the expected sequence.
//
// Packet encoding:
//   PCR packets  – adaptation field carries PCR whose base == pos.
//   Non-PCR pkts – payload-only, no adaptation field.
//   Both types   – position is stored at bytes [186:188] so tests can decode
//                  it without needing to know which type they are reading.

import (
	"bytes"
	"io"
	"net/http"
	"sort"
	"testing"

	"xteve/src/mpegts"
)

// pcrPkt returns a 188-byte PCR-carrying MPEG-TS packet.
// The PCR base equals pos; the position is also stored at bytes [186:188].
func pcrPkt(pos int) []byte {
	pkt := make([]byte, mpegts.PacketSize)
	pkt[0] = mpegts.SyncByte
	pkt[1] = 0x01
	pkt[2] = 0x00                     // PID 0x0100
	pkt[3] = 0x30 | byte(pos&0x0F)    // adaptation_field_control = 11 (both)
	pkt[4] = 7                         // adaptation field length
	pkt[5] = 0x10                      // PCR_flag
	base := int64(pos)
	pkt[6] = byte(base >> 25)
	pkt[7] = byte(base >> 17)
	pkt[8] = byte(base >> 9)
	pkt[9] = byte(base >> 1)
	pkt[10] = byte(base&0x01)<<7 | 0x7E
	pkt[11] = 0x00
	pkt[186] = byte(pos >> 8)
	pkt[187] = byte(pos & 0xFF)
	return pkt
}

// noPCRPkt returns a 188-byte payload-only MPEG-TS packet (no PCR).
// The position is stored at bytes [186:188].
func noPCRPkt(pos int) []byte {
	pkt := make([]byte, mpegts.PacketSize)
	pkt[0] = mpegts.SyncByte
	pkt[1] = 0x01
	pkt[2] = 0x01                  // PID 0x0101 (distinct from PCR packets)
	pkt[3] = 0x10 | byte(pos&0x0F) // adaptation_field_control = 01 (payload only)
	pkt[186] = byte(pos >> 8)
	pkt[187] = byte(pos & 0xFF)
	return pkt
}

// posFromPkt decodes the position stored at bytes [186:188] of any test packet.
func posFromPkt(pkt []byte) int {
	return int(pkt[186])<<8 | int(pkt[187])
}

// buildConnData concatenates the packets at the given positions into a byte
// slice suitable for use as an HTTP response body.
func buildConnData(all [][]byte, positions ...int) []byte {
	var buf bytes.Buffer
	for _, p := range positions {
		buf.Write(all[p])
	}
	return buf.Bytes()
}

// fakeTSResponse wraps raw TS data in a minimal *http.Response.
func fakeTSResponse(data []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

// readPositions reads every 188-byte packet from all .ts files in dir (sorted
// by filename) and returns their decoded positions in order.
func readPositions(t *testing.T, dir string) []int {
	t.Helper()
	entries, err := bufferVFS.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	pkt := make([]byte, mpegts.PacketSize)
	var out []int
	for _, e := range entries {
		f, err := bufferVFS.Open(dir + e.Name())
		if err != nil {
			t.Logf("open %s: %v (skipping)", e.Name(), err)
			continue
		}
		for {
			n, err := io.ReadFull(f, pkt)
			if n == mpegts.PacketSize {
				out = append(out, posFromPkt(pkt))
			}
			if err != nil {
				break
			}
		}
		f.Close()
	}
	return out
}

// runConn calls handleTSStream once with the supplied packet data, simulating
// one CDN connection.  It uses retries=0 so that an EOF triggers the "retry"
// path and saves the partial segment; the updated stream state (LastPCR,
// PacketsAfterLastPCR) propagates back to the caller via the stream pointer.
func runConn(t *testing.T, stream *ThisStream, streamID int, playlistID, tmpFolder string, tmpSegment *int, buf []byte, bw *BandwidthCalculation, data []byte) {
	t.Helper()
	resp := fakeTSResponse(data)
	var errs []error
	addErr := func(err error) { errs = append(errs, err) }
	// retries=0 → EOF causes handleTSStreamError to return true (retry),
	// which saves the partial segment and returns (isRedirect=true, nil).
	_, err := stream.handleTSStream(t.Context(), resp, streamID, playlistID, tmpFolder, tmpSegment, addErr, buf, bw, 0)
	if err != nil {
		t.Fatalf("handleTSStream: %v", err)
	}
	for _, e := range errs {
		t.Errorf("stream error: %v", e)
	}
}

// setupDedupTest initialises the VFS and shared state required by
// handleTSStream / completeTSsegment.  The returned stream pointer must be
// passed to every runConn call; cleanup must be called after assertions.
func setupDedupTest(t *testing.T, tag string) (stream *ThisStream, streamID int, playlistID, folder string, buf []byte, bw *BandwidthCalculation, cleanup func()) {
	t.Helper()

	initBufferVFS(true)

	prev := Settings
	Settings.BufferSize = 100 // 100 KB — no segment boundary fires mid-connection
	Settings.UserAgent = "xTeVe-PCR-Test"
	Settings.StreamRetryEnabled = true
	Settings.StreamMaxRetries = 20
	Settings.StreamRetryDelay = 0

	streamID = 0
	playlistID = "M-pcr-" + tag
	folder = "/tmp/xteve_pcr_" + tag + "/"

	streamURL := "http://pcr-dedup-test-" + tag
	md5Val, err := getMD5(streamURL)
	if err != nil {
		t.Fatalf("getMD5: %v", err)
	}

	s := ThisStream{
		URL:        streamURL,
		Folder:     folder,
		MD5:        md5Val,
		PlaylistID: playlistID,
	}
	pl := &Playlist{
		Folder:     folder,
		PlaylistID: playlistID,
		Streams:    map[int]ThisStream{streamID: s},
		Clients:    map[int]ThisClient{streamID: {Connection: 1}},
	}
	BufferInformation.Store(playlistID, pl)

	var cc ClientConnection
	cc.Connection = 1
	BufferClients.Store(playlistID+md5Val, &cc)

	if err := bufferVFS.MkdirAll(folder, 0755); err != nil {
		t.Fatalf("MkdirAll %s: %v", folder, err)
	}

	buf = make([]byte, 1024*Settings.BufferSize)
	bw = &BandwidthCalculation{}
	stream = &s

	cleanup = func() {
		BufferInformation.Delete(playlistID)
		BufferClients.Delete(playlistID + md5Val)
		_ = bufferVFS.RemoveAll(folder)
		Settings = prev
	}
	return
}

// TestPCRDeduplication_TrailingNonPCRPackets is the core regression test.
//
// Packet layout (P = PCR-carrying, N = no PCR):
//
//	pos:  0   1   2   3   4   5   6   7   8   9  10  11
//	type: P   N   P   N   N   P   N   P   N   P   N   N
//	pcr:  0   -   2   -   -   5   -   7   -   9   -   -
//
// Connections and overlap:
//
//	Conn 1: [0..5]  → lastPCR=5, packetsAfterLastPCR=0
//	Conn 2: [3..8]  → 3-pkt overlap (N3,N4,P5); new data: N6,P7,N8
//	                   lastPCR=7, packetsAfterLastPCR=1
//	Conn 3: [5..11] → 4-pkt overlap (P5,N6,P7,N8); new data: P9,N10,N11
//
// Expected output: positions 0–11 in order, with no duplicates and no gaps.
func TestPCRDeduplication_TrailingNonPCRPackets(t *testing.T) {
	stream, streamID, playlistID, folder, buf, bw, cleanup := setupDedupTest(t, "trail")
	defer cleanup()

	// Build the canonical packet table.
	pkts := [][]byte{
		pcrPkt(0),   // 0  PCR=0
		noPCRPkt(1), // 1
		pcrPkt(2),   // 2  PCR=2
		noPCRPkt(3), // 3
		noPCRPkt(4), // 4
		pcrPkt(5),   // 5  PCR=5  ← end of conn 1 (PacketsAfterLastPCR=0)
		noPCRPkt(6), // 6
		pcrPkt(7),   // 7  PCR=7
		noPCRPkt(8), // 8          ← end of conn 2 (PacketsAfterLastPCR=1)
		pcrPkt(9),   // 9  PCR=9
		noPCRPkt(10),// 10
		noPCRPkt(11),// 11         ← end of conn 3
	}

	tmpSeg := 1

	// Connection 1: full initial burst [0..5].
	runConn(t, stream, streamID, playlistID, folder, &tmpSeg, buf, bw,
		buildConnData(pkts, 0, 1, 2, 3, 4, 5))

	// Connection 2: CDN rewinds 3 packets — overlap is N3, N4, P5.
	runConn(t, stream, streamID, playlistID, folder, &tmpSeg, buf, bw,
		buildConnData(pkts, 3, 4, 5, 6, 7, 8))

	// Connection 3: CDN rewinds 4 packets — overlap is P5, N6, P7, N8.
	runConn(t, stream, streamID, playlistID, folder, &tmpSeg, buf, bw,
		buildConnData(pkts, 5, 6, 7, 8, 9, 10, 11))

	got := readPositions(t, folder)

	want := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	if len(got) != len(want) {
		t.Fatalf("position count: got %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("position[%d]: got %d, want %d  (full sequence: %v)", i, g, want[i], got)
			break
		}
	}
}

// TestPCRDeduplication_ExactPCRMatch tests that when PacketsAfterLastPCR == 0
// (the connection ended exactly on a PCR packet) the reconnect correctly skips
// just that one PCR packet and resumes with the next packet.
func TestPCRDeduplication_ExactPCRMatch(t *testing.T) {
	stream, streamID, playlistID, folder, buf, bw, cleanup := setupDedupTest(t, "exact")
	defer cleanup()

	// pos:  0   1   2   3   4   5   6   7
	// type: P   N   N   P   P   N   P   N
	// pcr:  0   -   -   3   4   -   6   -
	pkts := [][]byte{
		pcrPkt(0),   // 0  PCR=0
		noPCRPkt(1), // 1
		noPCRPkt(2), // 2
		pcrPkt(3),   // 3  PCR=3  ← conn 1 ends here (PacketsAfterLastPCR=0)
		pcrPkt(4),   // 4  PCR=4
		noPCRPkt(5), // 5
		pcrPkt(6),   // 6  PCR=6
		noPCRPkt(7), // 7
	}

	tmpSeg := 1

	// Conn 1: [0..3], ends on a PCR packet.
	runConn(t, stream, streamID, playlistID, folder, &tmpSeg, buf, bw,
		buildConnData(pkts, 0, 1, 2, 3))

	// Conn 2: CDN rewinds 2 — overlap is N2, P3; new data: P4, N5, P6, N7.
	runConn(t, stream, streamID, playlistID, folder, &tmpSeg, buf, bw,
		buildConnData(pkts, 2, 3, 4, 5, 6, 7))

	got := readPositions(t, folder)
	want := []int{0, 1, 2, 3, 4, 5, 6, 7}
	if len(got) != len(want) {
		t.Fatalf("position count: got %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("position[%d]: got %d, want %d  (full: %v)", i, g, want[i], got)
			break
		}
	}
}

// TestPCRDeduplication_CDNResumesAhead tests the "better than nothing" path:
// the CDN reconnects from AFTER our last PCR rather than rewinding.  No skip
// zone applies — the new packets must be accepted immediately.
func TestPCRDeduplication_CDNResumesAhead(t *testing.T) {
	stream, streamID, playlistID, folder, buf, bw, cleanup := setupDedupTest(t, "ahead")
	defer cleanup()

	// pos:  0   1   2   3   4   5   6   7
	// type: P   N   P   N   P   N   P   N
	// pcr:  0   -   2   -   4   -   6   -
	pkts := [][]byte{
		pcrPkt(0),   // 0  PCR=0
		noPCRPkt(1), // 1              ← conn 1 ends (LastPCR=0, packetsAfterLastPCR=1)
		pcrPkt(2),   // 2  PCR=2
		noPCRPkt(3), // 3              ← conn 2 ends (LastPCR=2, packetsAfterLastPCR=1)
		pcrPkt(4),   // 4  PCR=4
		noPCRPkt(5), // 5
		pcrPkt(6),   // 6  PCR=6
		noPCRPkt(7), // 7
	}

	tmpSeg := 1

	// Conn 1: CDN sends [0, 1], closes.
	runConn(t, stream, streamID, playlistID, folder, &tmpSeg, buf, bw,
		buildConnData(pkts, 0, 1))

	// Conn 2: CDN resumes at P2 — pcr=2 > lastPCR=0, so accepted immediately.
	runConn(t, stream, streamID, playlistID, folder, &tmpSeg, buf, bw,
		buildConnData(pkts, 2, 3))

	// Conn 3: CDN resumes at P4 — pcr=4 > lastPCR=2, accepted immediately.
	runConn(t, stream, streamID, playlistID, folder, &tmpSeg, buf, bw,
		buildConnData(pkts, 4, 5, 6, 7))

	got := readPositions(t, folder)
	want := []int{0, 1, 2, 3, 4, 5, 6, 7}
	if len(got) != len(want) {
		t.Fatalf("position count: got %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i, g := range got {
		if g != want[i] {
			t.Errorf("position[%d]: got %d, want %d  (full: %v)", i, g, want[i], got)
			break
		}
	}
}
