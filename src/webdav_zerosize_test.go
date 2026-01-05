package src

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWebDAVFS_ZeroSize(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "xteve_webdav_zerosize_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Save original values
	origFolderData := System.Folder.Data
	origFilesM3U := Settings.Files.M3U
	origStreamsAll := Data.Streams.All
	defer func() {
		System.Folder.Data = origFolderData
		Settings.Files.M3U = origFilesM3U
		Data.Streams.All = origStreamsAll
	}()

	System.Folder.Data = tempDir
	Settings.Files.M3U = make(map[string]interface{})

	// Mock server that returns 200 OK but 0 Content-Length on HEAD
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == "GET" {
			// Serve dummy content
			_, _ = w.Write([]byte("data"))
		}
	}))
	defer ts.Close()

	hash := "testhash_zerosize"
	ClearWebDAVCache(hash)
	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Test Playlist"}

	// Create dummy M3U file
	err = os.WriteFile(filepath.Join(tempDir, hash+".m3u"), []byte("#EXTM3U"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	Data.Streams.All = []interface{}{
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Zero Size Group",
			"name":         "Zero Size File",
			"url":          ts.URL,
			"_duration":    "3600", // VOD
		},
	}

	fs := &WebDAVFS{}
	ctx := context.Background()

	// Path to the file
	// /dav/<hash>/On Demand/Zero Size Group/Individual/Zero Size File.mp4
	path := "/" + hash + "/" + dirOnDemand + "/Zero Size Group/" + dirIndividual + "/Zero Size File.mp4"

	// Stat the file
	info, err := fs.Stat(ctx, path)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}

	if info.Size() == 0 {
		t.Errorf("File size is 0, expected > 0 (or fallback 1TB)")
	} else {
		t.Logf("File size: %d", info.Size())
	}
}
