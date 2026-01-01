package src

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWebDAVTimestamp(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "xteve_webdav_timestamp_test")
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

	// Create a dummy M3U file with a specific timestamp
	hash := "timestamphash"
	content := "#EXTM3U\n#EXTINF:-1 group-title=\"Test Group\",Test Stream\nhttp://test.com/stream.mp4"
	m3uPath := filepath.Join(tempDir, hash+".m3u")
	err = os.WriteFile(m3uPath, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Set a specific mod time (e.g., 1 hour ago)
	fixedTime := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	err = os.Chtimes(m3uPath, fixedTime, fixedTime)
	if err != nil {
		t.Fatal(err)
	}

	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Timestamp Playlist"}

	Data.Streams.All = []interface{}{
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Test Group",
			"name":         "Test Stream",
			"url":          "http://test.com/stream.mp4",
			"_duration":    "123", // VOD
		},
	}

	fs := &WebDAVFS{}
	ctx := context.Background()

	// Helper to check timestamp
	checkTimestamp := func(path string) {
		info, err := fs.Stat(ctx, path)
		if err != nil {
			t.Fatalf("Failed to stat %s: %v", path, err)
		}

		// fs.Stat() calls os.Stat() internally which returns system specific precision.
		// We use Truncate(Second) for comparison to be safe across FS types.
		if !info.ModTime().Truncate(time.Second).Equal(fixedTime.Truncate(time.Second)) {
			t.Errorf("Timestamp for %s mismatch. Expected %v, got %v", path, fixedTime, info.ModTime())
		}
	}

	// 1. Check Hash Dir
	checkTimestamp("/" + hash)

	// 2. Check On Demand Dir
	checkTimestamp("/" + hash + "/" + dirOnDemand)

	// 3. Check Group Dir
	checkTimestamp("/" + hash + "/" + dirOnDemand + "/Test Group")

	// 4. Check Individual Dir
	checkTimestamp("/" + hash + "/" + dirOnDemand + "/Test Group/" + dirIndividual)

	// 5. Check File
	checkTimestamp("/" + hash + "/" + dirOnDemand + "/Test Group/" + dirIndividual + "/Test Stream.mp4")

	// 6. Check Listing File
	checkTimestamp("/" + hash + "/" + fileListing)

    // Check Readdir timestamps
    f, err := fs.OpenFile(ctx, "/" + hash + "/" + dirOnDemand + "/Test Group/" + dirIndividual, os.O_RDONLY, 0)
    if err != nil {
        t.Fatalf("Failed to open Individual dir: %v", err)
    }
    infos, err := f.Readdir(-1)
    f.Close()
    if err != nil {
        t.Fatalf("Failed to readdir: %v", err)
    }
    found := false
    for _, info := range infos {
        if info.Name() == "Test Stream.mp4" {
            found = true
            if !info.ModTime().Truncate(time.Second).Equal(fixedTime.Truncate(time.Second)) {
                 t.Errorf("Readdir Timestamp for Test Stream.mp4 mismatch. Expected %v, got %v", fixedTime, info.ModTime())
            }
        }
    }
    if !found {
        t.Errorf("Test Stream.mp4 not found in Readdir")
    }
}
