package src

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDownloadFileFromServer_Limit(t *testing.T) {
	// Bypass SSRF protection for test
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	// Temporarily reduce limit
	originalLimit := maxProviderDownloadSize
	maxProviderDownloadSize = 1024 // 1KB
	defer func() { maxProviderDownloadSize = originalLimit }()

	// Start test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send 2KB of data
		data := make([]byte, 2048)
		w.Write(data)
	}))
	defer server.Close()

	// Call downloadFileFromServer
	_, _, err := downloadFileFromServer(context.Background(), server.URL)

	if err == nil {
		t.Error("Expected error due to size limit, got nil")
	} else {
		expectedErr := "file too large"
		if len(err.Error()) < len(expectedErr) || err.Error()[:len(expectedErr)] != expectedErr {
			t.Errorf("Expected error starting with %q, got %q", expectedErr, err.Error())
		}
	}
}

func TestDownloadFileFromServer_ContentLengthLimit(t *testing.T) {
	// Bypass SSRF protection for test
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	// Temporarily reduce limit
	originalLimit := maxProviderDownloadSize
	maxProviderDownloadSize = 1024 // 1KB
	defer func() { maxProviderDownloadSize = originalLimit }()

	// Start test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send Content-Length header > limit
		w.Header().Set("Content-Length", "2048")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ignore"))
	}))
	defer server.Close()

	// Call downloadFileFromServer
	_, _, err := downloadFileFromServer(context.Background(), server.URL)

	if err == nil {
		t.Error("Expected error due to size limit, got nil")
	} else {
		// Error should be from Content-Length check
		expectedErr := fmt.Sprintf("file too large: %d bytes (max: %d)", 2048, 1024)
		if err.Error() != expectedErr {
			t.Errorf("Expected error %q, got %q", expectedErr, err.Error())
		}
	}
}

func TestHandleHLSStream_Limit(t *testing.T) {
	// Temporarily reduce limit
	originalLimit := maxPlaylistDownloadSize
	maxPlaylistDownloadSize = 100 // 100 bytes
	defer func() { maxPlaylistDownloadSize = originalLimit }()

	// Construct a dummy response with Content-Length > limit
	w := httptest.NewRecorder()
	// write enough data
	data := make([]byte, 200)
	w.Write(data)
	resp := w.Result()
	// Ensure Content-Length is set (ResponseRecorder usually sets it if body is written)
	if resp.ContentLength == -1 {
		resp.ContentLength = 200
	}

	// Mock error handler
	var errReceived error
	errorHandler := func(err error) {
		errReceived = err
	}

	stream := ThisStream{}
	_, err := handleHLSStream(context.Background(), resp, stream, "/tmp", nil, errorHandler, "http://localhost/playlist.m3u8")

	if err == nil {
		t.Error("Expected error due to size limit, got nil")
	}
	if errReceived == nil {
		t.Error("Expected error passed to errorHandler, got nil")
	}
}
