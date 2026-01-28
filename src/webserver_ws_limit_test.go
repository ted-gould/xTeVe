package src

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWSReadLimit(t *testing.T) {
	// Reduce the limit for testing purposes
	// Note: websocketReadLimit must be defined in webserver.go
	originalLimit := websocketReadLimit
	websocketReadLimit = 1024 // 1 KB limit
	defer func() { websocketReadLimit = originalLimit }()

	// Start a test server with the WS handler
	s := httptest.NewServer(http.HandlerFunc(WS))
	defer s.Close()

	// Convert http URL to ws URL
	u := "ws" + strings.TrimPrefix(s.URL, "http")

	// Connect to the server
	ws, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer ws.Close()

	// Construct a message larger than the limit
	// 2048 bytes
	largeMessage := strings.Repeat("a", 2048)
    // We need to send valid JSON because the handler expects JSON
    // RequestStruct has a "cmd" field.
    jsonMsg := `{"cmd":"` + largeMessage + `"}`

    if int64(len(jsonMsg)) <= websocketReadLimit {
        t.Fatalf("Test message size %d is not larger than limit %d", len(jsonMsg), websocketReadLimit)
    }

	// Write the large message
	if err := ws.WriteMessage(websocket.TextMessage, []byte(jsonMsg)); err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Attempt to read response.
	// If the limit is enforced, the server should close the connection.
	// The client might receive a CloseError or an EOF.
	_, _, err = ws.ReadMessage()

    if err == nil {
        t.Error("Expected error (connection closed) due to message size limit, but got nil")
    } else {
        t.Logf("Got expected error: %v", err)
    }

    // Give server a moment to process and close
    time.Sleep(100 * time.Millisecond)
}
