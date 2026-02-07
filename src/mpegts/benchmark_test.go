package mpegts

import (
	"testing"
)

func BenchmarkParser_Next(b *testing.B) {
	data := make([]byte, PacketSize*1000)
	for i := 0; i < 1000; i++ {
		data[i*PacketSize] = SyncByte
	}

	parser := NewParser()
	// Initial fill
	_, _ = parser.Write(data)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Refill if empty
		if parser.buf.Len() < PacketSize {
			b.StopTimer()
			parser.buf.Reset()
			_, _ = parser.Write(data)
			b.StartTimer()
		}
		_, _ = parser.Next()
	}
}

func BenchmarkParser_NextInto(b *testing.B) {
	data := make([]byte, PacketSize*1000)
	for i := 0; i < 1000; i++ {
		data[i*PacketSize] = SyncByte
	}

	parser := NewParser()
	// Initial fill
	_, _ = parser.Write(data)
	packetBuf := make([]byte, PacketSize)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Refill if empty
		if parser.buf.Len() < PacketSize {
			b.StopTimer()
			parser.buf.Reset()
			_, _ = parser.Write(data)
			b.StartTimer()
		}
		_ = parser.NextInto(packetBuf)
	}
}
