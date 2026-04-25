package mpegts

import (
	"bytes"
	"io"
)

const (
	// PacketSize is the size of a single MPEG-TS packet.
	PacketSize = 188
	// SyncByte is the sync byte that marks the start of a packet.
	SyncByte = 0x47
)

// Parser is a parser for MPEG-TS streams.
type Parser struct {
	buf *bytes.Buffer
}

// NewParser creates a new MPEG-TS parser.
func NewParser() *Parser {
	return &Parser{
		buf: &bytes.Buffer{},
	}
}

// Write appends the given data to the parser's buffer.
func (p *Parser) Write(data []byte) (int, error) {
	return p.buf.Write(data)
}

// Next returns the next valid MPEG-TS packet from the buffer.
// If no packet is available, it returns io.EOF.
func (p *Parser) Next() ([]byte, error) {
	// Find the sync byte.
	idx := bytes.IndexByte(p.buf.Bytes(), SyncByte)
	if idx == -1 {
		// No sync byte found, so we can't find a packet.
		// We can discard the entire buffer.
		p.buf.Reset()
		return nil, io.EOF
	}

	// Discard any data before the sync byte.
	if idx > 0 {
		p.buf.Next(idx)
	}

	// Check if we have a full packet.
	if p.buf.Len() < PacketSize {
		return nil, io.EOF
	}

	packet := make([]byte, PacketSize)
	if _, err := p.buf.Read(packet); err != nil {
		return nil, err
	}

	return packet, nil
}

// ExtractPCR extracts the Program Clock Reference value from an MPEG-TS
// packet's adaptation field.  It returns (pcr, true) when the packet carries a
// valid PCR, or (0, false) when it does not.
//
// The returned value is in 27 MHz units (PCR_base×300 + PCR_ext) and fits
// comfortably in an int64.  Callers typically use it as a monotone stream-time
// reference to detect backward-overlap on live-stream reconnects.
func ExtractPCR(packet []byte) (pcr int64, ok bool) {
	if len(packet) < PacketSize {
		return 0, false
	}
	// Byte 3, bits 5-4: adaptation_field_control.
	// 0b10 = adaptation field only; 0b11 = adaptation field + payload.
	if packet[3]&0x30 < 0x20 {
		return 0, false // no adaptation field
	}
	// Byte 4: adaptation_field_length – must be ≥ 7 to hold the flags byte
	// plus the 6-byte PCR field.
	if packet[4] < 7 {
		return 0, false
	}
	// Byte 5 flags: bit 4 (0x10) is PCR_flag.
	if packet[5]&0x10 == 0 {
		return 0, false
	}
	// Bytes 6-11 encode the 42-bit PCR value:
	//   33-bit base  (90 kHz) in bits [47:15]
	//    6-bit reserved              in bits [14:9]
	//    9-bit extension (27 MHz)    in bits [8:0]
	b := packet[6:12]
	base := int64(b[0])<<25 | int64(b[1])<<17 | int64(b[2])<<9 |
		int64(b[3])<<1 | int64(b[4]>>7)
	ext := int64(b[4]&0x01)<<8 | int64(b[5])
	return base*300 + ext, true
}

// NextInto reads the next valid MPEG-TS packet into the provided buffer.
// The buffer must have a length of at least PacketSize.
// If no packet is available, it returns io.EOF.
func (p *Parser) NextInto(b []byte) error {
	if len(b) < PacketSize {
		return io.ErrShortBuffer
	}

	// Find the sync byte.
	idx := bytes.IndexByte(p.buf.Bytes(), SyncByte)
	if idx == -1 {
		// No sync byte found, so we can't find a packet.
		// We can discard the entire buffer.
		p.buf.Reset()
		return io.EOF
	}

	// Discard any data before the sync byte.
	if idx > 0 {
		p.buf.Next(idx)
	}

	// Check if we have a full packet.
	if p.buf.Len() < PacketSize {
		return io.EOF
	}

	if _, err := p.buf.Read(b[:PacketSize]); err != nil {
		return err
	}

	return nil
}
