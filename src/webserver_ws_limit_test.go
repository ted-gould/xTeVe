package src

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestWSMessageLimit(t *testing.T) {
	// Reduce limit for testing
	originalLimit := websocketReadLimit
	websocketReadLimit = 1024 // 1 KB
	defer func() { websocketReadLimit = originalLimit }()

	// Create test server with WS handler
	server := httptest.NewServer(http.HandlerFunc(WS))
	defer server.Close()

	// Convert http URL to ws URL
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect to WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	assert.NoError(t, err)
	defer ws.Close()

	// 1. Send small message (should pass)
	// We send a valid JSON to ensure it doesn't fail on parsing
	smallReq := RequestStruct{Cmd: "getServerConfig"}
	err = ws.WriteJSON(smallReq)
	assert.NoError(t, err)

	// Read response to ensure connection is alive and working
	// The WS handler sends a response for "getServerConfig" (though it's commented out in code? let's check)
    // Looking at WS handler:
    /*
		case "getServerConfig":
			// response.Config = Settings
    */
    // It doesn't write explicitly in the case block, but falls through to:
    /*
		if errWrite := conn.WriteJSON(&response); errWrite != nil {
    */
    // So it sends a response.
	_, _, err = ws.ReadMessage()
	assert.NoError(t, err, "Should be able to read response for small message")

	// 2. Send large message (should fail)
	// Create a large payload
	largePayload := make([]byte, 2048)
    for i := range largePayload {
        largePayload[i] = 'a'
    }
    // We wrap it in a JSON object just to be sure, though raw bytes count too.
    // {"cmd":"test", "padding":"aaaa..."}
    largeReq := map[string]string{
        "cmd": "test",
        "padding": string(largePayload),
    }

	err = ws.WriteJSON(largeReq)
	assert.NoError(t, err)

	// The server should detect the message is too big during read, and close the connection.
    // We try to read. We expect a CloseError 1009.

    // Note: Since WS handler runs in a loop, it might take a moment to process.
    ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = ws.ReadMessage()

    if err == nil {
        t.Fatal("Expected error when reading from closed connection, got nil. The server probably accepted the large message.")
    }

    // Check if it's the expected close error
    // Gorilla websocket usually returns a *CloseError
    if closeErr, ok := err.(*websocket.CloseError); ok {
        assert.Equal(t, websocket.CloseMessageTooBig, closeErr.Code, "Expected CloseMessageTooBig (1009)")
    } else {
        // Sometimes it might be a read error wrapping the close error or just "websocket: close 1009 (message too big)" string
        t.Logf("Received error: %v", err)
        assert.True(t, strings.Contains(err.Error(), "1009") || strings.Contains(err.Error(), "message too big"), "Error should indicate message too big")
    }
}
