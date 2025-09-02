package mpegts

import (
	"bytes"
	"io"
	"testing"
)

func TestParser_Next_ValidStream(t *testing.T) {
	// Create a buffer with two valid packets.
	var buf bytes.Buffer
	packet1 := make([]byte, PacketSize)
	packet1[0] = SyncByte
	packet1[1] = 0x11
	buf.Write(packet1)

	packet2 := make([]byte, PacketSize)
	packet2[0] = SyncByte
	packet2[1] = 0x22
	buf.Write(packet2)

	parser := NewParser()
	if _, err := parser.Write(buf.Bytes()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First packet
	p, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(p, packet1) {
		t.Errorf("expected packet %v, got %v", packet1, p)
	}

	// Second packet
	p, err = parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(p, packet2) {
		t.Errorf("expected packet %v, got %v", packet2, p)
	}

	// No more packets
	_, err = parser.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestParser_Next_CorruptedStream(t *testing.T) {
	// Create a buffer with garbage data and three valid packets.
	var buf bytes.Buffer
	buf.Write([]byte{0x01, 0x02, 0x03}) // Garbage

	packet1 := make([]byte, PacketSize)
	packet1[0] = SyncByte
	packet1[1] = 0x11
	buf.Write(packet1)

	buf.Write([]byte{0x04, 0x05, 0x06}) // More garbage

	packet2 := make([]byte, PacketSize)
	packet2[0] = SyncByte
	packet2[1] = 0x22
	buf.Write(packet2)

	packet3 := make([]byte, PacketSize)
	packet3[0] = SyncByte
	packet3[1] = 0x33
	buf.Write(packet3)

	parser := NewParser()
	if _, err := parser.Write(buf.Bytes()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First packet
	p, err := parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(p, packet1) {
		t.Errorf("expected packet %v, got %v", packet1, p)
	}

	// Second packet
	p, err = parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(p, packet2) {
		t.Errorf("expected packet %v, got %v", packet2, p)
	}

	// Third packet
	p, err = parser.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(p, packet3) {
		t.Errorf("expected packet %v, got %v", packet3, p)
	}

	// No more packets
	_, err = parser.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}
