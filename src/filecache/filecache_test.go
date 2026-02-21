package filecache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileCacheLRU(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filecache_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Ensure no env var is set for this test
	oldVal := os.Getenv("WEBDAV_CACHE_SIZE")
	os.Unsetenv("WEBDAV_CACHE_SIZE")
	defer func() {
		if oldVal != "" {
			os.Setenv("WEBDAV_CACHE_SIZE", oldVal)
		}
	}()

	Reset()
	fc, err := GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	maxItems := getMaxCacheItems()
	numFiles := maxItems + 5

	// Create items
	for i := 0; i < numFiles; i++ {
		hash := fmt.Sprintf("hash%d", i)
		path := filepath.Join(fc.dir, hash)

		// Create content file
		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		// Create metadata entry
		meta := Metadata{
			URL:      fmt.Sprintf("http://example.com/%d", i),
			Size:     4,
			ModTime:  time.Now(),
			CachedAt: time.Now(),
			Complete: true,
		}

		// Insert into DB manually to simulate usage
		_, err := fc.db.Exec(`INSERT OR REPLACE INTO metadata (hash, url, size, mod_time, etag, content_type) VALUES (?, ?, ?, ?, ?, ?)`,
			hash, meta.URL, meta.Size, meta.ModTime.UnixNano(), meta.ETag, meta.ContentType)
		if err != nil {
			t.Fatal(err)
		}

		// Insert into cache_files with access_time
		// Use older access time for first 5 items
		accessTime := time.Now().Add(-2 * time.Hour)
		if i >= 5 {
			accessTime = time.Now()
		}

		_, err = fc.db.Exec(`INSERT OR REPLACE INTO cache_files (hash, cached_at, complete, access_time) VALUES (?, ?, ?, ?)`,
			hash, meta.CachedAt.UnixNano(), meta.Complete, accessTime.UnixNano())
		if err != nil {
			t.Fatal(err)
		}
	}

	// Trigger cleanup
	fc.CleanNow()

	// Check count in cache_files
	var count int
	err = fc.db.QueryRow("SELECT COUNT(*) FROM cache_files").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count > maxItems {
		t.Errorf("Cache size %d exceeds max %d", count, maxItems)
	}

	// Verify items 0-4 are gone (Evicted)
	for i := 0; i < 5; i++ {
		hash := fmt.Sprintf("hash%d", i)
		path := filepath.Join(fc.dir, hash)

		// File should be gone
		if _, err := os.Stat(path); err == nil {
			t.Errorf("Item %s file should have been evicted", hash)
		}

		// Cache entry should be gone
		var exists bool
		err := fc.db.QueryRow("SELECT 1 FROM cache_files WHERE hash = ?", hash).Scan(&exists)
		if err != sql.ErrNoRows {
			t.Errorf("Item %s should have been removed from cache_files", hash)
		}
	}

	// Verify item 5 is present
	hash := fmt.Sprintf("hash%d", 5)
	path := filepath.Join(fc.dir, hash)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Item %s file should be present", hash)
	}

	// Verify item 5 in cache_files
	var exists bool
	err = fc.db.QueryRow("SELECT 1 FROM cache_files WHERE hash = ?", hash).Scan(&exists)
	if err != nil {
		t.Errorf("Item %s should be present in cache_files", hash)
	}
}

func TestFileCache_MetadataPersistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filecache_test_meta")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	Reset()
	fc, err := GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	maxItems := getMaxCacheItems()
	numFiles := maxItems + 5

	for i := 0; i < numFiles; i++ {
		hash := fmt.Sprintf("hash%d", i)
		path := filepath.Join(fc.dir, hash)

		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		meta := Metadata{
			URL:      fmt.Sprintf("http://example.com/%d", i),
			Size:     4,
			ModTime:  time.Now(),
			CachedAt: time.Now(),
			Complete: true,
		}

		// Insert metadata
		_, err := fc.db.Exec(`INSERT OR REPLACE INTO metadata (hash, url, size, mod_time, etag, content_type) VALUES (?, ?, ?, ?, ?, ?)`,
			hash, meta.URL, meta.Size, meta.ModTime.UnixNano(), meta.ETag, meta.ContentType)
		if err != nil {
			t.Fatal(err)
		}

		// Insert cache_files with access_time
		// Old access time for first 5
		accessTime := time.Now().Add(-2 * time.Hour)
		if i >= 5 {
			accessTime = time.Now()
		}
		_, err = fc.db.Exec(`INSERT OR REPLACE INTO cache_files (hash, cached_at, complete, access_time) VALUES (?, ?, ?, ?)`,
			hash, meta.CachedAt.UnixNano(), meta.Complete, accessTime.UnixNano())
		if err != nil {
			t.Fatal(err)
		}
	}

	// Trigger cleanup
	fc.CleanNow()

	// Verify oldest items (0-4) data file is gone, BUT metadata row remains
	for i := 0; i < 5; i++ {
		hash := fmt.Sprintf("hash%d", i)
		path := filepath.Join(fc.dir, hash)

		// Data file should be gone
		if _, err := os.Stat(path); err == nil {
			t.Errorf("Item %s data file should have been evicted", hash)
		}

		// Metadata row should REMAIN
		var exists bool
		err := fc.db.QueryRow("SELECT 1 FROM metadata WHERE hash = ?", hash).Scan(&exists)
		if err != nil {
			t.Errorf("Item %s metadata should still exist in DB", hash)
		}
	}
}

func TestMigration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filecache_test_migration")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Create JSON file and content file manually BEFORE GetInstance
	// GetInstance uses tempDir/xteve_cache
	// Wait, GetInstance logic:
	// cacheDir := filepath.Join(baseDir, "xteve_cache")

	cacheDir := filepath.Join(tempDir, "xteve_cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}

	hash := "testhash"
	contentPath := filepath.Join(cacheDir, hash)
	metaPath := filepath.Join(cacheDir, hash+".json")

	// Create content
	if err := os.WriteFile(contentPath, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create JSON
	// Note: mod_time and cached_at in JSON should be RFC3339 compatible for unmarshal if type is time.Time
	// In Metadata struct: ModTime time.Time `json:"mod_time"`
	// time.Time JSON unmarshal expects RFC3339 string.
	jsonContent := `{"url":"http://test.com","size":7,"mod_time":"2023-01-01T00:00:00Z","complete":true}`
	if err := os.WriteFile(metaPath, []byte(jsonContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Now Init FileCache, which should trigger migration
	Reset()
	fc, err := GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Verify JSON file is gone
	if _, err := os.Stat(metaPath); err == nil {
		t.Errorf("JSON file should have been deleted")
	}

	// Verify DB has entries
	var exists bool
	// Check metadata
	err = fc.db.QueryRow("SELECT 1 FROM metadata WHERE hash = ?", hash).Scan(&exists)
	if err != nil {
		t.Errorf("Metadata should exist in DB: %v", err)
	}

	// Check cache_files
	err = fc.db.QueryRow("SELECT 1 FROM cache_files WHERE hash = ?", hash).Scan(&exists)
	if err != nil {
		t.Errorf("Cache file entry should exist in DB: %v", err)
	}

	// Verify data correctness
	var url string
	var size int64
	if err := fc.db.QueryRow("SELECT url, size FROM metadata WHERE hash = ?", hash).Scan(&url, &size); err != nil {
		t.Fatalf("Failed to scan metadata: %v", err)
	}
	if url != "http://test.com" {
		t.Errorf("Expected URL http://test.com, got %s", url)
	}
	if size != 7 {
		t.Errorf("Expected size 7, got %d", size)
	}
}

func TestConfigurableCacheSize(t *testing.T) {
	tests := []struct {
		name           string
		envValue       string
		expectedSize   int
		shouldUseEnv   bool
	}{
		{
			name:         "default cache size when no env var",
			envValue:     "",
			expectedSize: DefaultMaxCacheItems,
			shouldUseEnv: false,
		},
		{
			name:         "custom cache size from env var",
			envValue:     "50",
			expectedSize: 50,
			shouldUseEnv: true,
		},
		{
			name:         "large cache size from env var",
			envValue:     "500",
			expectedSize: 500,
			shouldUseEnv: true,
		},
		{
			name:         "invalid env var falls back to default",
			envValue:     "not-a-number",
			expectedSize: DefaultMaxCacheItems,
			shouldUseEnv: true,
		},
		{
			name:         "zero env var falls back to default",
			envValue:     "0",
			expectedSize: DefaultMaxCacheItems,
			shouldUseEnv: true,
		},
		{
			name:         "negative env var falls back to default",
			envValue:     "-10",
			expectedSize: DefaultMaxCacheItems,
			shouldUseEnv: true,
		},
		{
			name:         "cache size at maximum limit",
			envValue:     "100000",
			expectedSize: MaxCacheItems,
			shouldUseEnv: true,
		},
		{
			name:         "cache size above maximum is capped",
			envValue:     "200000",
			expectedSize: MaxCacheItems,
			shouldUseEnv: true,
		},
		{
			name:         "extremely large value is capped",
			envValue:     "999999999",
			expectedSize: MaxCacheItems,
			shouldUseEnv: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldVal := os.Getenv("WEBDAV_CACHE_SIZE")
			defer func() {
				if oldVal != "" {
					os.Setenv("WEBDAV_CACHE_SIZE", oldVal)
				} else {
					os.Unsetenv("WEBDAV_CACHE_SIZE")
				}
			}()

			if tt.shouldUseEnv {
				os.Setenv("WEBDAV_CACHE_SIZE", tt.envValue)
			} else {
				os.Unsetenv("WEBDAV_CACHE_SIZE")
			}

			got := getMaxCacheItems()
			if got != tt.expectedSize {
				t.Errorf("getMaxCacheItems() = %d, want %d", got, tt.expectedSize)
			}
		})
	}
}

func TestConfigurableCacheSizeIntegration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filecache_test_size")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	oldVal := os.Getenv("WEBDAV_CACHE_SIZE")
	os.Setenv("WEBDAV_CACHE_SIZE", "25")
	defer func() {
		if oldVal != "" {
			os.Setenv("WEBDAV_CACHE_SIZE", oldVal)
		} else {
			os.Unsetenv("WEBDAV_CACHE_SIZE")
		}
	}()

	Reset()
	fc, err := GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	customMax := 25
	// Create 30 files
	for i := 0; i < 30; i++ {
		hash := fmt.Sprintf("hash%d", i)
		path := filepath.Join(fc.dir, hash)

		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}

		meta := Metadata{
			URL:      fmt.Sprintf("http://example.com/%d", i),
			Size:     4,
			ModTime:  time.Now(),
			CachedAt: time.Now(),
			Complete: true,
		}

		_, err := fc.db.Exec(`INSERT OR REPLACE INTO metadata (hash, url, size, mod_time, etag, content_type) VALUES (?, ?, ?, ?, ?, ?)`,
			hash, meta.URL, meta.Size, meta.ModTime.UnixNano(), meta.ETag, meta.ContentType)
		if err != nil {
			t.Fatal(err)
		}

		accessTime := time.Now()
		// Make first 5 older
		if i < 5 {
			accessTime = time.Now().Add(-1 * time.Hour)
		}

		_, err = fc.db.Exec(`INSERT OR REPLACE INTO cache_files (hash, cached_at, complete, access_time) VALUES (?, ?, ?, ?)`,
			hash, meta.CachedAt.UnixNano(), meta.Complete, accessTime.UnixNano())
		if err != nil {
			t.Fatal(err)
		}
	}

	fc.CleanNow()

	var count int
	if err := fc.db.QueryRow("SELECT COUNT(*) FROM cache_files").Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != customMax {
		t.Errorf("Cache size = %d, want %d", count, customMax)
	}

	// Verify oldest items (0-4) were evicted
	for i := 0; i < 5; i++ {
		hash := fmt.Sprintf("hash%d", i)
		var exists bool
		err := fc.db.QueryRow("SELECT 1 FROM cache_files WHERE hash = ?", hash).Scan(&exists)
		if err == nil {
			t.Errorf("Item %s should have been evicted", hash)
		}
	}
}
