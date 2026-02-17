package src

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebDAVRetryLogic(t *testing.T) {
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")
	os.Setenv("XTEVE_DISABLE_CACHE", "true")
	defer os.Unsetenv("XTEVE_DISABLE_CACHE")

	// Setup a test server that simulates a dropped connection
	fullContent := "0123456789"
	failedOnce := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")

		if !failedOnce {
			// First request: pretend to send everything but fail halfway
			// We set Content-Length to full size to trick client into expecting more
			w.Header().Set("Content-Length", strconv.Itoa(len(fullContent)))
			// We can't easily force a connection reset with httptest, but we can try
			// writing less than Content-Length and closing (which causes ErrUnexpectedEOF)

			// We need to flush to ensure client gets the first part
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("expected http.Flusher")
			}

			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(fullContent[:5])); err != nil {
				t.Errorf("failed to write initial content: %v", err)
			}
			flusher.Flush()

			// Now we want to simulate a failure.
			// If we just return, the server closes the chunk/stream.
			// Since we set Content-Length, the client should see io.ErrUnexpectedEOF
			failedOnce = true

			// Force close connection to ensure client sees error
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
			}
			return
		}

		// Retry request
		// Client should send Range header
		if rangeHeader == "" {
			t.Error("Expected Range header on retry")
			http.Error(w, "Missing Range", http.StatusBadRequest)
			return
		}

		// Parse Range: bytes=5-
		if !strings.HasPrefix(rangeHeader, "bytes=") {
			t.Errorf("Invalid Range header: %s", rangeHeader)
			return
		}
		parts := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
		start, _ := strconv.Atoi(parts[0])

		if start != 5 {
			t.Errorf("Expected start=5, got %d", start)
		}

		// Serve remaining
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(fullContent)-1, len(fullContent)))
		w.Header().Set("Content-Length", strconv.Itoa(len(fullContent)-start))
		w.WriteHeader(http.StatusPartialContent)
		if _, err := w.Write([]byte(fullContent[start:])); err != nil {
			t.Errorf("failed to write remaining content: %v", err)
		}
	}))
	defer server.Close()

	// Setup WebDAVFS with this stream
	// We'll manually construct the webdavStream because setting up the whole FS structure
	// with Data.Streams.All and Settings.Files.M3U is tedious and we just want to test the stream logic.
	// But webdavStream is private. So we have to go through WebDAVFS.

	// Setup minimal environment for WebDAVFS to find the stream
	tempDir, _ := os.MkdirTemp("", "xteve_retry_test")
	defer os.RemoveAll(tempDir)

	// Save original global state
	origFolderData := System.Folder.Data
	origFolderCache := System.Folder.Cache
	origFolderTemp := System.Folder.Temp
	origFilesM3U := Settings.Files.M3U
	origStreamsAll := Data.Streams.All
	defer func() {
		System.Folder.Data = origFolderData
		System.Folder.Cache = origFolderCache
		System.Folder.Temp = origFolderTemp
		Settings.Files.M3U = origFilesM3U
		Data.Streams.All = origStreamsAll
		// Reset global file cache so next test gets a fresh instance
		globalFileCache = nil
		globalFileCacheOnce = sync.Once{}
	}()

	System.Folder.Data = tempDir
	// Use a non-existent directory for cache to effectively disable caching
	System.Folder.Cache = tempDir + "/no-cache"
	System.Folder.Temp = tempDir + "/no-cache"
	// Reset global file cache to use new temp directory
	globalFileCache = nil
	globalFileCacheOnce = sync.Once{}
	Settings.Files.M3U = make(map[string]interface{})
	hash := "retryhash"
	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Retry Playlist"}

	// Create dummy M3U
	if err := os.WriteFile(tempDir+"/"+hash+".m3u", []byte("#EXTM3U"), 0644); err != nil {
		t.Fatalf("Failed to write dummy M3U: %v", err)
	}

	Data.Streams.All = []interface{}{
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "RetryGroup",
			"name":         "RetryStream",
			"url":          server.URL,
			"_duration":    "100", // VOD
		},
	}

	// We need to clear the cache because other tests might have populated it
	// and we are changing Data.Streams.All
	ClearWebDAVCache("")

	fs := &WebDAVFS{}
	ctx := context.Background()

	// Open the file
	// Path: /<hash>/On Demand/<Group>/Individual/<File>
	// File name needs extension
	f, err := fs.OpenFile(ctx, "/"+hash+"/On Demand/RetryGroup/Individual/RetryStream.mp4", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	defer f.Close()

	// Read from it
	start := time.Now()
	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	elapsed := time.Since(start)

	if string(content) != fullContent {
		t.Errorf("Expected content '%s', got '%s'", fullContent, string(content))
	}

	if !failedOnce {
		t.Error("Test didn't simulate failure")
	}

	// Verify that it didn't take too long (infinite loop check)
	if elapsed > 2*time.Second {
		t.Logf("Warning: ReadAll took %v", elapsed)
	}
}
