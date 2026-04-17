package src

import (
	"testing"
)

func TestParseM3U8_MultipleNewSegmentsLive(t *testing.T) {
	Settings.BufferSize = 1
	System.Flag.Debug = 0

	stream := ThisStream{}
	stream.URLStreamingServer = "http://example.com"
	stream.Status = false // Initial parsing

	// Playlist with sequence 1, 2, 3
	stream.Body = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:1\n#EXTINF:2.0,\n/seg1.ts\n#EXTINF:2.0,\n/seg2.ts\n#EXTINF:2.0,\n/seg3.ts\n"

	err := ParseM3U8(&stream)
	if err != nil {
		t.Fatalf("ParseM3U8 failed on initial run: %v", err)
	}

	// On the initial run (Status=false), it should pick up the last segment
	// except that if it is not a VOD stream, it strips off the last segment
	// of the playlist to avoid downloading a segment still being written to.
	// So we expect it to queue seg2 (sequence 2).
	if len(stream.Segment) != 1 {
		t.Fatalf("Expected 1 segment on initial run, got %d", len(stream.Segment))
	}
	if stream.LastSequence != 2 {
		t.Fatalf("Expected LastSequence to be 2, got %d", stream.LastSequence)
	}
	if stream.Segment[0].Sequence != 2 {
		t.Fatalf("Expected first queued segment sequence to be 2, got %d", stream.Segment[0].Sequence)
	}

	// Now simulate stream being active
	stream.Status = true

	// Consume the segment we just parsed
	stream.Segment = stream.Segment[1:]

	// Simulate server dropping 2 segments and adding 2 new ones (now seq 3, 4, 5)
	stream.Body = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:3\n#EXTINF:2.0,\n/seg3.ts\n#EXTINF:2.0,\n/seg4.ts\n#EXTINF:2.0,\n/seg5.ts\n"

	err = ParseM3U8(&stream)
	if err != nil {
		t.Fatalf("ParseM3U8 failed on second run: %v", err)
	}

	// Before the fix, the parser would break out after adding seg3 (seq 3) and LastSequence would be 3.
	// After the fix, it should queue seg3, seg4 AND seg5, and LastSequence should be 5.
	if len(stream.Segment) != 3 {
		t.Fatalf("Expected 3 new segments queued, got %d", len(stream.Segment))
	}
	if stream.LastSequence != 5 {
		t.Fatalf("Expected LastSequence to be 5, got %d", stream.LastSequence)
	}
	if stream.Segment[0].Sequence != 3 {
		t.Fatalf("Expected first queued segment sequence to be 3, got %d", stream.Segment[0].Sequence)
	}
	if stream.Segment[1].Sequence != 4 {
		t.Fatalf("Expected second queued segment sequence to be 4, got %d", stream.Segment[1].Sequence)
	}
	if stream.Segment[2].Sequence != 5 {
		t.Fatalf("Expected third queued segment sequence to be 5, got %d", stream.Segment[2].Sequence)
	}
}

func TestParseM3U8_VOD(t *testing.T) {
	Settings.BufferSize = 1
	System.Flag.Debug = 0

	stream := ThisStream{}
	stream.URLStreamingServer = "http://example.com"
	stream.Status = false

	// VOD Playlist with ENDLIST
	stream.Body = "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MEDIA-SEQUENCE:1\n#EXTINF:2.0,\n/seg1.ts\n#EXTINF:2.0,\n/seg2.ts\n#EXTINF:2.0,\n/seg3.ts\n#EXT-X-ENDLIST\n"

	err := ParseM3U8(&stream)
	if err != nil {
		t.Fatalf("ParseM3U8 failed: %v", err)
	}

	// For VOD, the early escape block detects it and appends all 3 segments
	// immediately since the stream is static and fully available.
	if len(stream.Segment) != 3 {
		t.Fatalf("Expected 3 segments on VOD initial run, got %d", len(stream.Segment))
	}
	if stream.Segment[0].Sequence != 1 {
		t.Fatalf("Expected first queued segment sequence to be 1, got %d", stream.Segment[0].Sequence)
	}
}
