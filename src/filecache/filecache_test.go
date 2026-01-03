package filecache

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheManager_Integration(t *testing.T) {
	// Setup temp dir
	tmpDir, err := os.MkdirTemp("", "filecache_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cm, err := NewCacheManager(tmpDir)
	require.NoError(t, err)

	// Mock Server
	content := []byte("Hello, World! This is a test file for caching.")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "test.txt", time.Now(), bytes.NewReader(content))
	}))
	defer server.Close()

	url := server.URL

	// 1. Background Download
	ctx := context.Background()
	cm.BackgroundDownload(ctx, url, "UserAgent")

	// Wait for cache
	require.Eventually(t, func() bool {
		return cm.Exists(url)
	}, 2*time.Second, 100*time.Millisecond)

	// Verify Metadata
	meta, err := cm.GetMetadata(url)
	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), meta.Size)
	assert.Equal(t, url, meta.URL)

	// Verify Content
	f, meta2, err := cm.Get(url)
	require.NoError(t, err)
	defer f.Close()

	readContent, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, content, readContent)
	assert.Equal(t, meta, meta2)
}

func TestCacheManager_Eviction(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "filecache_evict_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cm, err := NewCacheManager(tmpDir)
	require.NoError(t, err)

	// Create dummy files
	for i := 0; i < MaxCacheItems+10; i++ {
		url := "http://example.com/" + string(rune(i))

		// Manually create file to avoid HTTP overhead
		path := cm.GetPath(url)
		err := os.WriteFile(path, []byte("data"), 0644)
		require.NoError(t, err)

		// Create metadata
		meta := Metadata{Size: 4, URL: url, ModTime: time.Now().Add(time.Duration(i) * time.Second)}
		err = cm.writeMetadata(url, meta)
		require.NoError(t, err)

		// Set ModTime explicitly to ensure order
		err = os.Chtimes(path, meta.ModTime, meta.ModTime)
		require.NoError(t, err)
	}

	// Trigger Eviction
	cm.Evict()

	// Check count
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	// Count data files (excluding json)
	count := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			count++
		}
	}

	assert.LessOrEqual(t, count, MaxCacheItems)
}
