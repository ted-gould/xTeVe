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
