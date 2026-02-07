package src

import (
	"fmt"
	"strings"
	"testing"
)

func generateM3U8Body(segments int) string {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-TARGETDURATION:10\n")
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")

	for i := 0; i < segments; i++ {
		sb.WriteString("#EXTINF:10.0,\n")
		sb.WriteString(fmt.Sprintf("segment_%d.ts\n", i))
	}

	sb.WriteString("#EXT-X-ENDLIST\n")
	return sb.String()
}

func BenchmarkParseM3U8(b *testing.B) {
	// Setup large body once
	body := generateM3U8Body(5000)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stream := &ThisStream{
			Body: body,
			// Initialize map to avoid nil panic if code accesses it
			DynamicStream: make(map[int]DynamicStream),
		}

		err := ParseM3U8(stream)
		if err != nil {
			b.Fatalf("ParseM3U8 failed: %v", err)
		}
	}
}
