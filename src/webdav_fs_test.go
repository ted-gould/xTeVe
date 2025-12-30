package src

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWebDAVFS(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "xteve_webdav_test")
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

	// Create a dummy M3U file
	hash := "testhash"
	content := "#EXTM3U\n#EXTINF:-1 group-title=\"Test Group\",Test Stream\nhttp://test.com/stream.mp4"
	err = os.WriteFile(filepath.Join(tempDir, hash+".m3u"), []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Test Playlist"}

	// Populate Data.Streams.All manually for "On Demand" testing
	Data.Streams.All = []interface{}{
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Test Group",
			"name":         "Test Stream",
			"url":          "http://test.com/stream.mp4",
			"_duration":    "123", // VOD
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "",
			"name":         "Uncategorized Stream",
			"url":          "http://test.com/other.mp4",
			// No duration, but mp4 extension -> VOD
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Live Group",
			"name":         "Live Channel",
			"url":          "http://test.com/live.m3u8",
			"_duration":    "-1", // Live
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Live Group 2",
			"name":         "Live Channel 2",
			"url":          "http://test.com/live.ts",
			// No duration, but ts extension -> Live
		},
	}

	fs := &WebDAVFS{}
	ctx := context.Background()

	// Test root listing
	f, err := fs.OpenFile(ctx, "/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open root: %v", err)
	}
	infos, err := f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read root dir: %v", err)
	}
	f.Close()

	foundHash := false
	for _, info := range infos {
		if info.Name() == hash {
			foundHash = true
			break
		}
	}
	if !foundHash {
		t.Errorf("Root listing did not contain hash directory %s", hash)
	}

	// Test hash directory listing
	f, err = fs.OpenFile(ctx, "/"+hash, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open hash dir: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read hash dir: %v", err)
	}
	f.Close()

	foundListing := false
	foundOnDemand := false
	for _, info := range infos {
		if info.Name() == "listing.m3u" {
			foundListing = true
		}
		if info.Name() == "On Demand" && info.IsDir() {
			foundOnDemand = true
		}
	}
	if !foundListing {
		t.Errorf("Hash dir listing did not contain listing.m3u")
	}
	if !foundOnDemand {
		t.Errorf("Hash dir listing did not contain 'On Demand'")
	}

	// Test On Demand listing
	f, err = fs.OpenFile(ctx, "/"+hash+"/On Demand", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open On Demand dir: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read On Demand dir: %v", err)
	}
	f.Close()

	foundGroup := false
	foundUncat := false
	foundLiveGroup := false
	for _, info := range infos {
		if info.Name() == "Test Group" && info.IsDir() {
			foundGroup = true
		}
		if info.Name() == "Uncategorized" && info.IsDir() {
			foundUncat = true
		}
		if info.Name() == "Live Group" {
			foundLiveGroup = true
		}
	}
	if !foundGroup {
		t.Errorf("On Demand listing did not contain 'Test Group'")
	}
	if !foundUncat {
		t.Errorf("On Demand listing did not contain 'Uncategorized'")
	}
	if foundLiveGroup {
		t.Errorf("On Demand listing contained 'Live Group' which should be filtered out")
	}

	// Test Group listing
	f, err = fs.OpenFile(ctx, "/"+hash+"/On Demand/Test Group", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open Group dir: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read Group dir: %v", err)
	}
	f.Close()

	foundStream := false
	for _, info := range infos {
		if info.Name() == "Test_Stream.mp4" && !info.IsDir() {
			foundStream = true
		}
	}
	if !foundStream {
		t.Errorf("Group listing did not contain 'Test_Stream.mp4'")
	}

	// Test stream opening (check stat)
	f, err = fs.OpenFile(ctx, "/"+hash+"/On Demand/Test Group/Test_Stream.mp4", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open stream file: %v", err)
	}

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Failed to stat stream file: %v", err)
	}
	if info.Name() != "Test_Stream.mp4" {
		t.Errorf("Stream name mismatch. Got %s, want Test_Stream.mp4", info.Name())
	}
	f.Close()

	// Test nonexistent
	_, err = fs.OpenFile(ctx, "/"+hash+"/On Demand/Nonexistent", os.O_RDONLY, 0)
	if !os.IsNotExist(err) {
		t.Errorf("Expected NotExist for nonexistent group/file")
	}
}
