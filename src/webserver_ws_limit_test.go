package src

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWSLimit(t *testing.T) {
	// 1. Setup small limit
	oldLimit := websocketReadLimit
	websocketReadLimit = 1024 // 1KB
	defer func() { websocketReadLimit = oldLimit }()

	// 2. Setup Settings
	// We ensure AuthenticationWEB is false so we don't need to authenticate
	oldAuth := Settings.AuthenticationWEB
	Settings.AuthenticationWEB = false
	defer func() { Settings.AuthenticationWEB = oldAuth }()

	oldWizard := System.ConfigurationWizard
	System.ConfigurationWizard = false
	defer func() { System.ConfigurationWizard = oldWizard }()

	// 3. Start Server
	// We need to suppress logs during test or ignore them
	server := httptest.NewServer(http.HandlerFunc(WS))
	defer server.Close()

	// 4. Connect
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// 5. Send large message
	// 2048 bytes > 1024 bytes limit
	// We construct a valid JSON to pass unmarshaling if the limit wasn't there
	payload := strings.Repeat("A", 2048)
	err = conn.WriteJSON(map[string]string{
        "cmd": "test",
        "data": payload,
    })
	if err != nil {
		// It is possible write fails if server closes very fast, but usually it succeeds
		t.Logf("Write failed: %v", err)
	}

	// 6. Wait for server to close or respond
	// With the fix, the server should close the connection because the message is too big.
	// Without the fix, the server should process the message and send a response.

    conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn.ReadMessage()

	if err == nil {
		t.Error("Expected error (connection closed) due to message size limit, but got nil (success)")
	} else {
        // Verify it's a close error if possible, but any error indicates the connection didn't survive
        if websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
             t.Logf("Got expected CloseMessageTooBig error")
        } else {
             t.Logf("Got error: %v", err)
        }
    }
}
