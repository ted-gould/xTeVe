package src

import (
	"fmt"
	"strings"
	"testing"
)

func generateM3U8(segments int) string {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-TARGETDURATION:10\n")
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	for i := 0; i < segments; i++ {
		sb.WriteString(fmt.Sprintf("#EXTINF:10.0,\n"))
		sb.WriteString(fmt.Sprintf("segment_%d.ts\n", i))
	}
	sb.WriteString("#EXT-X-ENDLIST\n")
	return sb.String()
}

func BenchmarkParseM3U8(b *testing.B) {
	// Setup
	segments := 10000
	body := generateM3U8(segments)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stream := &ThisStream{
			Body:               body,
			M3U8URL:            "http://example.com/playlist.m3u8",
			URLStreamingServer: "http://example.com/",
		}
		err := ParseM3U8(stream)
		if err != nil {
			b.Fatalf("ParseM3U8 failed: %v", err)
		}
		if len(stream.Segment) != segments {
			b.Fatalf("Expected %d segments, got %d", segments, len(stream.Segment))
		}
	}
}
