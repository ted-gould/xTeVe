package src

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWebDAVCacheRefresh(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "xteve_webdav_cache_test")
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
		// Clean up global cache
		ClearGroupCache("")
	}()

	System.Folder.Data = tempDir
	Settings.Files.M3U = make(map[string]interface{})

	// Create a dummy M3U file
	hash := "testhash_cache"
	content := "#EXTM3U\n#EXTINF:-1 group-title=\"Group A\",Stream A\nhttp://test.com/a.mp4"
	err = os.WriteFile(filepath.Join(tempDir, hash+".m3u"), []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Test Cache Playlist"}

	// Populate Data.Streams.All manually for "On Demand" testing
	Data.Streams.All = []interface{}{
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Group A",
			"name":         "Stream A",
			"url":          "http://test.com/a.mp4",
			"_duration":    "123", // VOD
		},
	}

	fs := &WebDAVFS{}
	ctx := context.Background()

	// 1. Access WebDAV groups to populate the cache.
	f, err := fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open On Demand dir: %v", err)
	}
	infos, err := f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read On Demand dir: %v", err)
	}
	f.Close()

	foundGroupA := false
	for _, info := range infos {
		if info.Name() == "Group A" {
			foundGroupA = true
			break
		}
	}
	if !foundGroupA {
		t.Errorf("Initial listing did not contain 'Group A'")
	}

	// Verify cache is populated
	groupCacheMutex.RLock()
	cachedGroups, ok := groupCache[hash]
	groupCacheMutex.RUnlock()
	if !ok {
		t.Errorf("Cache was not populated")
	}
	if len(cachedGroups) != 1 || cachedGroups[0] != "Group A" {
		t.Errorf("Cache content incorrect: %v", cachedGroups)
	}

	// 2. Modify the underlying Data.Streams.All (simulating an update).
	Data.Streams.All = []interface{}{
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Group B",
			"name":         "Stream B",
			"url":          "http://test.com/b.mp4",
			"_duration":    "123", // VOD
		},
	}

	// NOTE: If we query now WITHOUT clearing cache, we should still see Group A
	// (This confirms caching is actually working effectively "stale" until cleared)
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open On Demand dir: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read On Demand dir: %v", err)
	}
	f.Close()

	foundGroupA = false
	foundGroupB := false
	for _, info := range infos {
		if info.Name() == "Group A" {
			foundGroupA = true
		}
		if info.Name() == "Group B" {
			foundGroupB = true
		}
	}
	if !foundGroupA {
		t.Errorf("Cache should still have 'Group A' before clearing")
	}
	if foundGroupB {
		t.Errorf("Cache should NOT have 'Group B' before clearing")
	}

	// 3. Call ClearGroupCache("").
	ClearGroupCache("")

	// Verify cache is cleared
	groupCacheMutex.RLock()
	_, ok = groupCache[hash]
	groupCacheMutex.RUnlock()
	if ok {
		t.Errorf("Cache was not cleared")
	}

	// 4. Access WebDAV groups again and verify that the new groups are returned.
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open On Demand dir: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read On Demand dir: %v", err)
	}
	f.Close()

	foundGroupA = false
	foundGroupB = false
	for _, info := range infos {
		if info.Name() == "Group A" {
			foundGroupA = true
		}
		if info.Name() == "Group B" {
			foundGroupB = true
		}
	}

	if foundGroupA {
		t.Errorf("Listing contained 'Group A' after update (should be gone)")
	}
	if !foundGroupB {
		t.Errorf("Listing did not contain 'Group B' after update")
	}
}
