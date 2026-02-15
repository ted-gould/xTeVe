package src

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"xteve/src/filecache"
)

func TestWebDAVFS_SizeFallback_Triggered(t *testing.T) {
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	// Setup FileCache
	filecache.Reset()
	tmpDir := t.TempDir()
	System.Folder.Cache = tmpDir
	System.Folder.Temp = tmpDir
	Settings.UserAgent = "xTeVe/Test"

	// Mock Server
	realSize := int64(10 * 1024 * 1024) // 10MB
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			// Return 200 OK but no Content-Length (simulating unknown size from HEAD)
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == "GET" {
			// Check if Range request
			rangeHeader := r.Header.Get("Range")
			// Depending on filecache.MaxFileSize, it might be 1048576 or something else.
			// Currently code uses filecache.MaxFileSize-1.
			// Let's just check prefix "bytes=0-"
			if len(rangeHeader) > 8 && rangeHeader[:8] == "bytes=0-" {
				// Expect "bytes=0-1048575"
				// Content-Range format: "bytes start-end/total"
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-1048575/%d", realSize))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(make([]byte, 1048576)) // Send 1MB of zeros
				return
			}
			// If request is specifically for last bytes (which shouldn't happen in triggered test if 1st works), handle it just in case logic changed
			if rangeHeader == "bytes=-1024" {
				start := realSize - 1024
				end := realSize - 1
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, realSize))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(make([]byte, 1024))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer ts.Close()

	// Call fetchRemoteMetadataFunc directly
	ctx := context.Background()
	meta, err := fetchRemoteMetadataFunc(ctx, ts.URL)
	if err != nil {
		t.Fatalf("fetchRemoteMetadata failed: %v", err)
	}

	// Verify Size
	expectedSize := realSize
	if meta.Size != expectedSize {
		t.Errorf("Expected size %d, got %d. Fallback logic was skipped or failed.", expectedSize, meta.Size)
	}
}

func TestWebDAVFS_SizeFallback_LastBytes(t *testing.T) {
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	// Setup FileCache
	filecache.Reset()
	tmpDir := t.TempDir()
	System.Folder.Cache = tmpDir
	System.Folder.Temp = tmpDir
	Settings.UserAgent = "xTeVe/Test"

	// Mock Server
	realSize := int64(10 * 1024 * 1024) // 10MB
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			// Fail HEAD
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.Method == "GET" {
			rangeHeader := r.Header.Get("Range")
			// Match prefix for first MB check
			if len(rangeHeader) > 8 && rangeHeader[:8] == "bytes=0-" {
				// First request: unknown size
				w.Header().Set("Content-Range", "bytes 0-1048575/*")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(make([]byte, 1048576))
				return
			}
			if rangeHeader == "bytes=-1024" {
				// Last bytes request: returns size
				start := realSize - 1024
				end := realSize - 1
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, realSize))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(make([]byte, 1024))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	meta, err := fetchRemoteMetadataFunc(ctx, ts.URL)
	if err != nil {
		t.Fatalf("fetchRemoteMetadata failed: %v", err)
	}

	expectedSize := realSize
	if meta.Size != expectedSize {
		t.Errorf("Expected size %d, got %d. Fallback to last bytes logic failed.", expectedSize, meta.Size)
	}
}
