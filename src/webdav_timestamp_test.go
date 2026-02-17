package src

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWebDAVTimestamp(t *testing.T) {
	// Mock fetchRemoteMetadataFunc
	origFetchRemoteMetadataFunc := fetchRemoteMetadataFunc

	// Set a specific remote modtime (e.g., 4 hours ago) to simulate Last-Modified from remote
	remoteModTime := time.Now().Add(-4 * time.Hour).Truncate(time.Second)
	fetchRemoteMetadataFunc = func(ctx context.Context, urlStr string) (FileMeta, error) {
		// Return remote modtime (simulating Last-Modified header from remote server)
		return FileMeta{Size: 1024, ModTime: remoteModTime}, nil
	}
	defer func() { fetchRemoteMetadataFunc = origFetchRemoteMetadataFunc }()

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

	// Set a specific mod time for M3U (e.g., 2 hours ago)
	m3uTime := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	err = os.Chtimes(m3uPath, m3uTime, m3uTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a dummy JSON file with a DIFFERENT timestamp (e.g., 1 hour ago)
	// AND store a specific mod_time inside the JSON (e.g., 3 hours ago)
	jsonContentTime := time.Now().Add(-3 * time.Hour).Truncate(time.Second)
	jsonContent := fmt.Sprintf(`{"mod_time": "%s"}`, jsonContentTime.Format(time.RFC3339))

	jsonPath := filepath.Join(tempDir, hash+".json")
	err = os.WriteFile(jsonPath, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	jsonFileModTime := time.Now().Add(-1 * time.Hour).Truncate(time.Second)
	err = os.Chtimes(jsonPath, jsonFileModTime, jsonFileModTime)
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
	checkTimestamp := func(path string, expectedTime time.Time) {
		info, err := fs.Stat(ctx, path)
		if err != nil {
			t.Fatalf("Failed to stat %s: %v", path, err)
		}

		// fs.Stat() calls os.Stat() internally which returns system specific precision.
		// We use Truncate(Second) for comparison to be safe across FS types.
		if !info.ModTime().Truncate(time.Second).Equal(expectedTime.Truncate(time.Second)) {
			t.Errorf("Timestamp for %s mismatch. Expected %v, got %v", path, expectedTime, info.ModTime())
		}
	}

	// All checks should return the remote timestamp when available, not the JSON/M3U timestamp
	// This ensures we use Last-Modified from remote servers instead of falling back to M3U modtime

	// 1. Check Hash Dir - uses JSON content timestamp for directory itself
	checkTimestamp("/" + hash, jsonContentTime)

	// 2. Check On Demand Dir - uses JSON content timestamp for directory itself
	checkTimestamp("/" + hash + "/" + dirOnDemand, jsonContentTime)

	// 3. Check Group Dir - uses JSON content timestamp for directory itself
	checkTimestamp("/" + hash + "/" + dirOnDemand + "/Test Group", jsonContentTime)

	// 4. Check Individual Dir - uses JSON content timestamp for directory itself
	checkTimestamp("/" + hash + "/" + dirOnDemand + "/Test Group/" + dirIndividual, jsonContentTime)

	// 5. Check File - should use REMOTE timestamp, not JSON timestamp
	checkTimestamp("/" + hash + "/" + dirOnDemand + "/Test Group/" + dirIndividual + "/Test Stream.mp4", remoteModTime)

	// 6. Check Listing File
	// Listing file is the actual M3U content, so its timestamp should be from the M3U file?
	// But `statHashSubDir` uses `getM3UModTime` for everything EXCEPT `fileListing`.
	// For `fileListing`, it explicitly stats the M3U file:
	// "info, err := os.Stat(realPath)" -> this gets M3U file stats directly.
	// So listing file should retain M3U timestamp unless we change that logic too.
	// The user requested "verify that we use the date from the json file in the cache... when returning dates through webdav".
	// Usually this refers to directory timestamps and metadata, but let's check listing file separately.
	// Current impl:
	// if sub == fileListing {
	//		realPath := filepath.Join(System.Folder.Data, hash+".m3u")
	//		info, err := os.Stat(realPath) ... return info.ModTime()
	// }
	// So listing file will use M3U time. I'll assert M3U time for listing file, and JSON time for dirs.
	checkTimestamp("/" + hash + "/" + fileListing, m3uTime)

    // Check Readdir timestamps - should use remote timestamp
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
            if !info.ModTime().Truncate(time.Second).Equal(remoteModTime.Truncate(time.Second)) {
                 t.Errorf("Readdir Timestamp for Test Stream.mp4 mismatch. Expected %v (remote), got %v", remoteModTime, info.ModTime())
            }
        }
    }
    if !found {
        t.Errorf("Test Stream.mp4 not found in Readdir")
    }
}
