package mpegts

import (
	"testing"
)

func BenchmarkParser_Next(b *testing.B) {
	// Create dummy data: 100 packets
	data := make([]byte, 100*PacketSize)
	for i := 0; i < 100; i++ {
		data[i*PacketSize] = SyncByte
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser := NewParser()
		// We need to keep writing data because Next() consumes it
		// For the benchmark, we can simulate a continuous stream or just reset
		// But allocating a new parser every time is also overhead.
		// Let's test the Next() call specifically.

		parser.Write(data)

		for {
			_, err := parser.Next()
			if err != nil {
				break
			}
		}
	}
}

func BenchmarkParser_Next_Alloc(b *testing.B) {
	// Setup a parser with a lot of data so we don't have to Write() constantly
	// but Parser buffers everything in memory, so we can't make it too huge.
	// Actually, let's just measure one Next() call cost if possible,
	// but the parser state changes.

	// Better approach: Re-use the parser and reset the buffer?
	// The Parser struct doesn't expose Reset, but we can just make a new one.

	packet := make([]byte, PacketSize)
	packet[0] = SyncByte

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p := NewParser()
		p.Write(packet)
		_, _ = p.Next()
	}
}

func BenchmarkParser_NextInto(b *testing.B) {
	data := make([]byte, 100*PacketSize)
	for i := 0; i < 100; i++ {
		data[i*PacketSize] = SyncByte
	}
	packetBuf := make([]byte, PacketSize)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		parser := NewParser()
		parser.Write(data)

		for {
			_, err := parser.NextInto(packetBuf)
			if err != nil {
				break
			}
		}
	}
}

func BenchmarkParser_NextInto_Alloc(b *testing.B) {
	packet := make([]byte, PacketSize)
	packet[0] = SyncByte
	packetBuf := make([]byte, PacketSize)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		p := NewParser()
		p.Write(packet)
		_, _ = p.NextInto(packetBuf)
	}
}
