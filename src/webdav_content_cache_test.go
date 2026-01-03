package src

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"xteve/src/filecache"
)

func TestWebDAVContentCache(t *testing.T) {
	// 1. Setup Mock Server
	// Serve a file larger than 1MB (e.g. 2MB)
	fileContent := make([]byte, 2*1024*1024)
	for i := range fileContent {
		fileContent[i] = byte(i % 256)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle Range requests
		http.ServeContent(w, r, "video.mp4", time.Now(), bytes.NewReader(fileContent))
	}))
	defer ts.Close()

	// 2. Setup xTeVe environment
	tempDir, err := os.MkdirTemp("", "xteve_content_cache_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	origFolderCache := System.Folder.Cache
	origFolderTemp := System.Folder.Temp
	origStreamsAll := Data.Streams.All
	origFilesM3U := Settings.Files.M3U

	System.Folder.Cache = tempDir
	System.Folder.Temp = tempDir

	// Reset global cache
	filecache.Reset()
	globalFileCache = nil
	globalFileCacheOnce = sync.Once{}

	defer func() {
		System.Folder.Cache = origFolderCache
		System.Folder.Temp = origFolderTemp
		Data.Streams.All = origStreamsAll
		Settings.Files.M3U = origFilesM3U
		filecache.Reset() // cleanup
	}()

	// 3. Setup M3U
	hash := "contenthash"
	Settings.Files.M3U = make(map[string]interface{})
	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Test Content Cache"}

	// 4. Configure stream
	stream := map[string]string{
		"_file.m3u.id": hash,
		"group-title":  "Group C",
		"name":         "Stream C",
		"url":          ts.URL, // Point to mock server
		"_duration":    "123",
	}
	Data.Streams.All = []interface{}{stream}

	fs := &WebDAVFS{}
	ctx := context.Background()

	// 5. Open file via WebDAV
	// Path: /<hash>/On Demand/Group C/Individual/Stream C.mp4
	f, err := fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Group C/"+dirIndividual+"/Stream C.mp4", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	// 6. Read first 100 bytes
	buf := make([]byte, 100)
	n, err := f.Read(buf)
	if err != nil {
		f.Close()
		t.Fatalf("Read failed: %v", err)
	}
	if n != 100 {
		f.Close()
		t.Errorf("Expected 100 bytes, got %d", n)
	}

	// Verify content
	if !bytes.Equal(buf, fileContent[:100]) {
		f.Close()
		t.Errorf("Read content mismatch")
	}
	f.Close()

	// At this point, StartCaching should have been triggered.
	// Wait a bit for background download (it might be fast or slow)
	// We'll poll
	fc := getFileCache()
	var exists bool
	var meta *filecache.Metadata
	var cachePath string

	for i := 0; i < 20; i++ {
		cachePath, meta, exists = fc.Get(ts.URL)
		if exists {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !exists {
		t.Errorf("File should be in cache by now")
	} else {
		// Verify metadata
		if meta.Size != int64(len(fileContent)) {
			t.Errorf("Expected size %d, got %d", len(fileContent), meta.Size)
		}
		if meta.Complete {
			t.Errorf("Expected incomplete cache (max 1MB)")
		}

		// Verify cache file size
		info, err := os.Stat(cachePath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != 1024*1024 {
			t.Errorf("Expected cache file size 1MB, got %d", info.Size())
		}
	}

	// 7. Verify subsequent read uses cache
	f2, err := fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Group C/"+dirIndividual+"/Stream C.mp4", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile 2 failed: %v", err)
	}
	defer f2.Close()

	// Seek to 500KB (inside cache)
	if _, err := f2.Seek(500*1024, io.SeekStart); err != nil {
		t.Fatalf("Seek failed: %v", err)
	}

	buf2 := make([]byte, 100)
	n, err = f2.Read(buf2)
	if err != nil {
		t.Fatalf("Read 2 failed: %v", err)
	}
	if n != 100 {
		t.Errorf("Expected 100 bytes, got %d", n)
	}
	if !bytes.Equal(buf2, fileContent[500*1024:500*1024+100]) {
		t.Errorf("Read 2 content mismatch")
	}

	// Seek to 1.5MB (outside cache)
	if _, err := f2.Seek(1500*1024, io.SeekStart); err != nil {
		t.Fatalf("Seek outside cache failed: %v", err)
	}

	buf3 := make([]byte, 100)
	n, err = f2.Read(buf3)
	if err != nil {
		t.Fatalf("Read outside cache failed: %v", err)
	}
	if n != 100 {
		t.Errorf("Expected 100 bytes, got %d", n)
	}
	if !bytes.Equal(buf3, fileContent[1500*1024:1500*1024+100]) {
		t.Errorf("Read outside cache content mismatch")
	}
}
