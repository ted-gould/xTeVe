package src

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkParseM3U8(b *testing.B) {
	// Create a sample M3U8 body with many segments
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-TARGETDURATION:10\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	sb.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n") // Force VOD

	numSegments := 10000
	for i := 0; i < numSegments; i++ {
		sb.WriteString("#EXTINF:10.0,\n")
		sb.WriteString(fmt.Sprintf("http://example.com/segment%d.ts\n", i))
	}
	sb.WriteString("#EXT-X-ENDLIST\n")

	body := sb.String()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stream := &ThisStream{
			Body: body,
		}

		err := ParseM3U8(stream)
		if err != nil {
			b.Fatalf("ParseM3U8 failed: %v", err)
		}

		if len(stream.Segment) != numSegments {
			b.Fatalf("Expected %d segments, got %d", numSegments, len(stream.Segment))
		}
	}
}
