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

func TestWebSocket_ReadLimit(t *testing.T) {
	// Setup xTeVe settings for test
	// We need to ensure global variables are set to allow bypassing auth
	// And we should restore them after test to avoid side effects
	originalAuth := Settings.AuthenticationWEB
	originalWizard := System.ConfigurationWizard
	originalImagesUpload := System.Folder.ImagesUpload

	t.Cleanup(func() {
		Settings.AuthenticationWEB = originalAuth
		System.ConfigurationWizard = originalWizard
		System.Folder.ImagesUpload = originalImagesUpload
	})

	Settings.AuthenticationWEB = false
	System.ConfigurationWizard = false
	System.Folder.ImagesUpload = t.TempDir() + "/"

	// Create test server with WS handler
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WS(w, r)
	}))
	defer s.Close()

	// Convert http URL to ws URL
	u := "ws" + strings.TrimPrefix(s.URL, "http")

	// Connect to the server
	// Increase handshake timeout just in case
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	ws, _, err := dialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("Failed to connect to websocket: %v", err)
	}
	defer ws.Close()

	// 1. Test small message (should pass)
	smallMsg := map[string]string{"cmd": "getServerConfig"}
	err = ws.WriteJSON(smallMsg)
	assert.NoError(t, err, "Should be able to write small message")

	// Read response (server sends response for getServerConfig)
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = ws.ReadMessage()
	assert.NoError(t, err, "Should be able to read response for small message")

	// 2. Test large message (should fail after fix)
	// 33MB string
	largePayload := strings.Repeat("a", 33*1024*1024)
	largeMsg := map[string]string{
		"cmd":    "uploadLogo",
		"base64": largePayload,
		"filename": "test.png",
	}

	// WriteJSON might succeed as it writes to the buffer/socket
	err = ws.WriteJSON(largeMsg)
	// We don't check error here because depending on OS buffer size, it might succeed or fail immediately if connection closed.

	// Attempt to read response.
	// BEFORE FIX: The server reads the whole message, processes it, and likely sends a response (error or success).
	//             So ReadMessage will succeed (err == nil) or timeout.
	// AFTER FIX:  The server closes the connection immediately when limit is exceeded.
	//             So ReadMessage will return a CloseError (err != nil).

	ws.SetReadDeadline(time.Now().Add(10 * time.Second)) // Give enough time for 33MB transfer if it happens
	_, _, err = ws.ReadMessage()

	// We verify that an error occurred (Connection Closed).
	// This assertion should FAIL before the fix (showing the vulnerability/lack of limit),
	// and PASS after the fix.
	assert.Error(t, err, "Connection should be closed due to read limit")

	if err != nil {
		t.Logf("Got expected error: %v", err)
	} else {
		t.Log("Did not get error (connection remained open), vulnerability present.")
	}
}
