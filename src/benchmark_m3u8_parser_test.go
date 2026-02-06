package src_test

import (
	"fmt"
	"strings"
	"testing"
	"xteve/src"
)

func BenchmarkParseM3U8(b *testing.B) {
	// Generate a large M3U8 body
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-TARGETDURATION:10\n")
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")

	segmentCount := 10000
	for i := 0; i < segmentCount; i++ {
		sb.WriteString(fmt.Sprintf("#EXTINF:10.0,\n"))
		sb.WriteString(fmt.Sprintf("segment_%d.ts\n", i))
	}
	sb.WriteString("#EXT-X-ENDLIST\n")

	body := sb.String()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Create a new stream object for each iteration to avoid state carry-over
		// explicitly setting Status to false to mimic initial load or VOD
		stream := &src.ThisStream{
			Body:   body,
			Status: false,
		}

		err := src.ParseM3U8(stream)
		if err != nil {
			b.Fatalf("ParseM3U8 failed: %v", err)
		}
	}
}
