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
			rangeHeader := r.Header.Get("Range")
			// Front range: bytes=0-{MaxFileSize-1}
			if len(rangeHeader) > 8 && rangeHeader[:8] == "bytes=0-" {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", filecache.MaxFileSize-1, realSize))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(make([]byte, filecache.MaxFileSize))
				return
			}
			// Tail range: bytes=-{TailCacheSize} (negative index)
			if len(rangeHeader) > 6 && rangeHeader[:6] == "bytes=" && rangeHeader[6] == '-' {
				tailSize := filecache.TailCacheSize
				if int64(tailSize) > realSize {
					tailSize = int(realSize)
				}
				start := realSize - int64(tailSize)
				end := realSize - 1
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, realSize))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(make([]byte, tailSize))
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
			// Front range: returns unknown total size (*)
			if len(rangeHeader) > 8 && rangeHeader[:8] == "bytes=0-" {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/*", filecache.MaxFileSize-1))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(make([]byte, filecache.MaxFileSize))
				return
			}
			// Tail range: returns actual size via negative index
			if len(rangeHeader) > 6 && rangeHeader[:6] == "bytes=" && rangeHeader[6] == '-' {
				tailSize := filecache.TailCacheSize
				if int64(tailSize) > realSize {
					tailSize = int(realSize)
				}
				start := realSize - int64(tailSize)
				end := realSize - 1
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, realSize))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(make([]byte, tailSize))
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
