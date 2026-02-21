package src

import (
	"context"
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
	// actualCacheDir := filepath.Join(cacheDir, "xteve_cache")

	hash := "priorityhash"
	targetURL := "http://test.com/video.mp4"
	// urlHash := filecache.HashURL(targetURL)

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

	// Helper to reset cache
	resetCache := func() {
		filecache.Reset()
		globalFileCache = nil
		globalFileCacheOnce = sync.Once{}
		// Re-init fc because Reset closes it
		var err error
		fc, err = filecache.GetInstance(cacheDir)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Helper to run Stat and check time
	checkTime := func(name string, expected time.Time, stream map[string]string) {
		t.Helper()
		fs := &WebDAVFS{}
		// Populate Data.Streams.All
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
	t.Run("Step 6: M3U File Time", func(t *testing.T) {
		resetCache()
		// Mock remote fetch to fail
		fetchRemoteMetadataFunc = func(ctx context.Context, urlStr string) (FileMeta, error) {
			return FileMeta{}, fmt.Errorf("remote failed")
		}

		// Ensure no metadata in DB
		fc.RemoveAll() // Clear DB

		checkTime("Video.mp4", step6Time, baseStream)
	})

	// Step 5: Metadata CachedAt Time (formerly JSON File Stat Time)
	step5Time := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("Step 5: Metadata CachedAt Time", func(t *testing.T) {
		resetCache()
		fc.RemoveAll()

		// Write metadata with CachedAt but zero ModTime
		meta := filecache.Metadata{
			URL:      targetURL,
			CachedAt: step5Time,
			// ModTime zero
		}
		if err := fc.WriteMetadata(targetURL, meta); err != nil {
			t.Fatalf("Failed to write metadata: %v", err)
		}

		checkTime("Video.mp4", step5Time, baseStream)
	})

	// Step 4: Remote GET Time
	step4Time := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("Step 4: Remote GET Time", func(t *testing.T) {
		resetCache()
		fc.RemoveAll()

		fetchRemoteMetadataFunc = func(ctx context.Context, urlStr string) (FileMeta, error) {
			return FileMeta{ModTime: step4Time, Size: 123}, nil
		}

		checkTime("Video.mp4", step4Time, baseStream)
	})

	// Step 2: M3U Attribute Time
	step2Time := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("Step 2: M3U Attribute Time", func(t *testing.T) {
		resetCache()
		fc.RemoveAll()

		streamWithTime := make(map[string]string)
		for k, v := range baseStream {
			streamWithTime[k] = v
		}
		streamWithTime["time"] = step2Time.Format(time.RFC3339)

		checkTime("Video.mp4", step2Time, streamWithTime)
	})

	// Step 1: JSON Cache Content Time (Stored Metadata ModTime)
	step1Time := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("Step 1: Cache Metadata ModTime", func(t *testing.T) {
		resetCache()
		fc.RemoveAll()

		// Write metadata with valid ModTime
		meta := filecache.Metadata{URL: targetURL, ModTime: step1Time, Size: 999}
		if err := fc.WriteMetadata(targetURL, meta); err != nil {
			t.Fatal(err)
		}

		// Even if M3U has time, Step 1 (Cache) should override
		streamWithTime := make(map[string]string)
		for k, v := range baseStream {
			streamWithTime[k] = v
		}
		streamWithTime["time"] = step2Time.Format(time.RFC3339)

		checkTime("Video.mp4", step1Time, streamWithTime)
	})
}
