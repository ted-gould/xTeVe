package src

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"xteve/src/filecache"
)

func TestWebDAVPriority(t *testing.T) {
	// Setup temporary directories
	tempDir, err := os.MkdirTemp("", "xteve_priority_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Mock System configuration
	origFolderData := System.Folder.Data
	origFolderCache := System.Folder.Cache
	origFilesM3U := Settings.Files.M3U
	origStreamsAll := Data.Streams.All
	origFetchFunc := fetchRemoteMetadataFunc

	defer func() {
		System.Folder.Data = origFolderData
		System.Folder.Cache = origFolderCache
		Settings.Files.M3U = origFilesM3U
		Data.Streams.All = origStreamsAll
		fetchRemoteMetadataFunc = origFetchFunc
		filecache.Reset()
		// Reset WebDAV global cache
		globalFileCache = nil
		globalFileCacheOnce = sync.Once{}
	}()

	System.Folder.Data = tempDir
	System.Folder.Cache = cacheDir
	filecache.Reset()
	globalFileCache = nil
	globalFileCacheOnce = sync.Once{}

	// Initialize filecache
	fc, err := filecache.GetInstance(cacheDir)
	if err != nil {
		t.Fatal(err)
	}

	// The actual cache directory includes xteve_cache
	actualCacheDir := filepath.Join(cacheDir, "xteve_cache")

	hash := "priorityhash"
	targetURL := "http://test.com/video.mp4"
	urlHash := filecache.HashURL(targetURL)

	// Create dummy M3U file
	m3uPath := filepath.Join(tempDir, hash+".m3u")
	if err := os.WriteFile(m3uPath, []byte("#EXTM3U"), 0644); err != nil {
		t.Fatal(err)
	}
	// Set M3U file time (Step 6 fallback)
	step6Time := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(m3uPath, step6Time, step6Time); err != nil {
		t.Fatal(err)
	}

	Settings.Files.M3U = map[string]interface{}{
		hash: map[string]interface{}{"name": "Priority Test"},
	}

	// Helper to run Stat and check time
	checkTime := func(name string, expected time.Time, stream map[string]string) {
		t.Helper()
		fs := &WebDAVFS{}
		// We use resolveFileMetadata directly via Stat on a file
		// Construct path: /hash/On Demand/Group/Individual/File.mp4
		// We need to mock findIndividualStream to return our stream
		// But findIndividualStream uses getIndividualStreamFiles which uses Data.Streams.All
		// So we populate Data.Streams.All

		Data.Streams.All = []interface{}{stream}
		// Clear webdav cache to force re-evaluation
		ClearWebDAVCache(hash)

		path := fmt.Sprintf("/%s/%s/Group/%s/%s", hash, dirOnDemand, dirIndividual, name)
		info, err := fs.Stat(context.Background(), path)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if !info.ModTime().Equal(expected) {
			t.Errorf("Expected time %v, got %v", expected, info.ModTime())
		}
	}

	baseStream := map[string]string{
		"_file.m3u.id": hash,
		"group-title":  "Group",
		"name":         "Video",
		"url":          targetURL,
		"_duration":    "100", // VOD
	}

	// Step 6: Verify Fallback to M3U File Time
	// No other metadata exists.
	t.Run("Step 6: M3U File Time", func(t *testing.T) {
		// Mock remote fetch to fail/return nothing
		fetchRemoteMetadataFunc = func(ctx context.Context, urlStr string) (FileMeta, error) {
			return FileMeta{}, fmt.Errorf("remote failed")
		}

		// Ensure no cache file
		p := filepath.Join(actualCacheDir, urlHash+".json")
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			t.Fatalf("Failed to remove cache file: %v", err)
		}
		filecache.Reset() // Reset in-memory cache

		checkTime("Video.mp4", step6Time, baseStream)
	})

	// Step 5: JSON File Creation Time
	// Create a JSON cache file but with NO mod_time inside.
	step5Time := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("Step 5: JSON File Stat Time", func(t *testing.T) {
		jsonPath := filepath.Join(actualCacheDir, urlHash+".json")
		// Write valid JSON but with zero time.
		meta := filecache.Metadata{URL: targetURL} // ModTime is zero
		data, _ := json.Marshal(meta)
		if err := os.WriteFile(jsonPath, data, 0644); err != nil {
			t.Fatalf("Failed to write json: %v", err)
		}
		if err := os.Chtimes(jsonPath, step5Time, step5Time); err != nil {
			t.Fatalf("Failed to chtimes: %v", err)
		}

		checkTime("Video.mp4", step5Time, baseStream)
	})

	// Step 4: Remote GET Time
	step4Time := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("Step 4: Remote GET Time", func(t *testing.T) {
		// Remove cache file
		p := filepath.Join(actualCacheDir, urlHash+".json")
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			t.Fatalf("Failed to remove cache file: %v", err)
		}

		// Mock fetchRemoteMetadataFunc to simulate HEAD failure but GET success
		// But wait, resolveMetadataFromRemote calls fetchRemoteMetadataFunc which returns ONE result.
		// We can just simulate it returning success.
		// Steps 3 and 4 are combined in resolveMetadataFromRemote.
		// If fetchRemoteMetadataFunc returns a time, it is used.
		// So checking Step 3 vs Step 4 separation is tricky unless we mock http client.
		// But logically, if we return a time here, it corresponds to Step 3/4.

		fetchRemoteMetadataFunc = func(ctx context.Context, urlStr string) (FileMeta, error) {
			return FileMeta{ModTime: step4Time, Size: 123}, nil
		}

		checkTime("Video.mp4", step4Time, baseStream)
	})

	// Step 2: M3U Attribute Time
	// We skip Step 3 (Remote) verification as it's same function as Step 4 from FS perspective.
	step2Time := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("Step 2: M3U Attribute Time", func(t *testing.T) {
		// Remove cache file
		p := filepath.Join(actualCacheDir, urlHash+".json")
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			t.Fatalf("Failed to remove cache file: %v", err)
		}

		// Add attribute to stream
		streamWithTime := make(map[string]string)
		for k, v := range baseStream {
			streamWithTime[k] = v
		}
		streamWithTime["time"] = step2Time.Format(time.RFC3339)

		// Even if remote fetch is available (from previous test setup), Step 2 should override Step 3?
		// Code:
		// Step 2 check. If found -> finalModTime = m3uTime.
		// Step 3 check. If finalModTime is Zero -> fetch remote.
		// So Step 2 prevents Step 3.
		// We keep fetchRemoteMetadataFunc returning step4Time.

		checkTime("Video.mp4", step2Time, streamWithTime)
	})

	// Step 1: JSON Cache Content Time
	step1Time := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("Step 1: JSON Cache Content Time", func(t *testing.T) {
		// Create JSON cache file with specific time
		meta := filecache.Metadata{URL: targetURL, ModTime: step1Time, Size: 999}
		if err := fc.WriteMetadata(targetURL, meta); err != nil {
			t.Fatal(err)
		}

		// Reset in-memory cache in WebDAVFS (webdavCache)?
		// The test helper clears it.
		// But filecache might have it in memory?
		// GetMetadata checks disk.

		// Even if M3U has time (from previous test stream), Step 1 should override.
		streamWithTime := make(map[string]string)
		for k, v := range baseStream {
			streamWithTime[k] = v
		}
		streamWithTime["time"] = step2Time.Format(time.RFC3339)

		checkTime("Video.mp4", step1Time, streamWithTime)
	})
}
