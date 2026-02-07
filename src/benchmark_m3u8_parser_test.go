package src

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkParseM3U8(b *testing.B) {
	// Generate a large M3U8 playlist
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	sb.WriteString("#EXT-X-TARGETDURATION:10\n")
	sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")

	numSegments := 10000
	for i := 0; i < numSegments; i++ {
		sb.WriteString("#EXTINF:10.0,\n")
		sb.WriteString(fmt.Sprintf("segment_%d.ts\n", i))
	}

	content := sb.String()

	// Create a stream with the generated content
	// We need to initialize the maps to avoid potential nil pointer dereferences if the code uses them
	stream := ThisStream{
		Body:          content,
		M3U8URL:       "http://example.com/playlist.m3u8",
		DynamicStream: make(map[int]DynamicStream),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Create a copy for each iteration to act like a fresh call
		s := stream
		// We don't care about the result in stream.Segment for this benchmark,
		// we care about the allocations during the function execution.
		_ = ParseM3U8(&s)
	}
}
