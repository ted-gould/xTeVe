package src

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWebSocket_ReadLimit(t *testing.T) {
	// Initialize minimal system settings required for WS handler
	Settings.AuthenticationWEB = false
	System.ConfigurationWizard = false

	// Create a test server with the WS handler
	s := httptest.NewServer(http.HandlerFunc(WS))
	defer s.Close()

	// Convert http:// to ws://
	u := "ws" + strings.TrimPrefix(s.URL, "http")

	// Connect to the server
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 45 * time.Second,
	}

	c, _, err := dialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// 32MB limit. We send 33MB.
	limit := int64(33554432)
	size := limit + 1024*1024 // 33MB

	w, err := c.NextWriter(websocket.TextMessage)
	if err != nil {
		t.Fatalf("NextWriter: %v", err)
	}

	// Write start of JSON
	w.Write([]byte(`{"cmd":"test", "data":"`))

	// Write junk data in chunks
	chunkSize := 1024 * 1024
	chunk := make([]byte, chunkSize)
	for i := range chunk {
		chunk[i] = 'A'
	}

	// Write 33 chunks
	for i := 0; i < int(size)/chunkSize; i++ {
		if _, err := w.Write(chunk); err != nil {
			// If write fails, it might mean server closed connection.
			t.Logf("Write failed at chunk %d: %v", i, err)
			break
		}
	}

	// Write end of JSON
	w.Write([]byte(`"}`))

	if err := w.Close(); err != nil {
		t.Logf("Close writer failed: %v", err)
	}

	// Try to read response
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = c.ReadMessage()

	if err == nil {
		t.Fatal("Server accepted >32MB message (Vulnerable)")
	} else {
		// Check if it's a timeout (meaning server kept connection open processing huge data)
		if strings.Contains(err.Error(), "i/o timeout") {
			t.Fatal("Server kept connection open (Vulnerable)")
		}
		// If it's a close error, it means server rejected it (Safe)
		t.Logf("Got expected error (Safe): %v", err)
	}
}
