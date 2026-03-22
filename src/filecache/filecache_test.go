package filecache

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	t.Setenv("SNAP_COMMON", "")

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

	t.Setenv("SNAP_COMMON", "")

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

	t.Setenv("SNAP_COMMON", "")

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
		name         string
		envValue     string
		expectedSize int
		shouldUseEnv bool
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
	t.Setenv("SNAP_COMMON", "")

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

func TestTailCacheDownload(t *testing.T) {
	// Create a 10MB file on a test server
	fileSize := 10 * 1024 * 1024
	fileContent := make([]byte, fileSize)
	for i := range fileContent {
		fileContent[i] = byte(i % 256)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "video.mp4", time.Now(), bytes.NewReader(fileContent))
	}))
	defer ts.Close()

	tempDir, err := os.MkdirTemp("", "filecache_tail_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	t.Setenv("SNAP_COMMON", "")

	Reset()
	fc, err := GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Start tail caching
	fc.StartTailCaching(ts.URL, int64(fileSize), http.DefaultClient, "")

	// Wait for download to complete
	var tailPath string
	var tailExists bool
	for i := 0; i < 50; i++ {
		tailPath, tailExists = fc.GetTail(ts.URL)
		if tailExists {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !tailExists {
		t.Fatal("Tail cache should exist after download")
	}

	// Verify file size: should be TailCacheSize (4MB)
	info, err := os.Stat(tailPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != TailCacheSize {
		t.Errorf("Expected tail file size %d, got %d", TailCacheSize, info.Size())
	}

	// Verify content: should be last 4MB of the file
	tailData, err := os.ReadFile(tailPath)
	if err != nil {
		t.Fatal(err)
	}
	expectedStart := fileSize - TailCacheSize
	if !bytes.Equal(tailData, fileContent[expectedStart:]) {
		t.Error("Tail cache content does not match expected file tail")
	}
}

func TestTailCacheSkipSmallFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filecache_tail_skip_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	t.Setenv("SNAP_COMMON", "")

	Reset()
	fc, err := GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// File size <= MaxFileSize: no tail should be created
	fc.StartTailCaching("http://example.com/small.mp4", MaxFileSize, http.DefaultClient, "")

	time.Sleep(200 * time.Millisecond)

	_, exists := fc.GetTail("http://example.com/small.mp4")
	if exists {
		t.Error("Tail cache should not be created for files <= MaxFileSize")
	}
}

func TestGetTail(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filecache_get_tail_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	t.Setenv("SNAP_COMMON", "")

	Reset()
	fc, err := GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	url := "http://example.com/video.mp4"
	tailHash := TailHash(url)

	// Pre-create tail file and DB entry
	tailPath := filepath.Join(fc.dir, tailHash)
	if err := os.WriteFile(tailPath, []byte("tail data"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = fc.db.Exec(`INSERT INTO cache_files (hash, cached_at, complete, access_time) VALUES (?, ?, ?, ?)`,
		tailHash, time.Now().UnixNano(), true, time.Now().Add(-1*time.Hour).UnixNano())
	if err != nil {
		t.Fatal(err)
	}

	// Retrieve it
	path, exists := fc.GetTail(url)
	if !exists {
		t.Fatal("GetTail should find pre-created entry")
	}
	if path != tailPath {
		t.Errorf("Expected path %s, got %s", tailPath, path)
	}

	// Verify access_time was updated (should be recent)
	var accessTime int64
	err = fc.db.QueryRow("SELECT access_time FROM cache_files WHERE hash = ?", tailHash).Scan(&accessTime)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(time.Unix(0, accessTime)) > 5*time.Second {
		t.Error("Access time should have been updated to now")
	}
}

func TestLRUWithTailFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filecache_lru_tail_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Limit is 5 URL pairs
	oldVal := os.Getenv("WEBDAV_CACHE_SIZE")
	os.Setenv("WEBDAV_CACHE_SIZE", "5")
	defer func() {
		if oldVal != "" {
			os.Setenv("WEBDAV_CACHE_SIZE", oldVal)
		} else {
			os.Unsetenv("WEBDAV_CACHE_SIZE")
		}
	}()
	t.Setenv("SNAP_COMMON", "")

	Reset()
	fc, err := GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create 7 URL pairs (front + tail each). That's 7 URLs > limit of 5.
	// URLs 0-1 have old access times (should be evicted).
	// URLs 2-6 have recent access times (should remain).
	for i := 0; i < 7; i++ {
		frontHash := fmt.Sprintf("url%d", i)
		tailHash := fmt.Sprintf("url%d_tail", i)

		// Create front file
		if err := os.WriteFile(filepath.Join(fc.dir, frontHash), []byte("front"), 0644); err != nil {
			t.Fatal(err)
		}
		// Create tail file
		if err := os.WriteFile(filepath.Join(fc.dir, tailHash), []byte("tail"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := fc.db.Exec(`INSERT OR REPLACE INTO metadata (hash, url, size) VALUES (?, ?, ?)`,
			frontHash, fmt.Sprintf("http://example.com/%d", i), 1000)
		if err != nil {
			t.Fatal(err)
		}

		accessTime := time.Now()
		if i < 2 {
			accessTime = time.Now().Add(-2 * time.Hour) // old
		}

		_, err = fc.db.Exec(`INSERT OR REPLACE INTO cache_files (hash, cached_at, complete, access_time) VALUES (?, ?, ?, ?)`,
			frontHash, time.Now().UnixNano(), true, accessTime.UnixNano())
		if err != nil {
			t.Fatal(err)
		}
		_, err = fc.db.Exec(`INSERT OR REPLACE INTO cache_files (hash, cached_at, complete, access_time) VALUES (?, ?, ?, ?)`,
			tailHash, time.Now().UnixNano(), true, accessTime.UnixNano())
		if err != nil {
			t.Fatal(err)
		}
	}

	fc.CleanNow()

	// Should have 5 URL pairs remaining = 10 cache_files rows
	var count int
	if err := fc.db.QueryRow("SELECT COUNT(*) FROM cache_files").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 10 {
		t.Errorf("Expected 10 cache_files rows (5 pairs), got %d", count)
	}

	// URLs 0-1 should be fully evicted (both front and tail)
	for i := 0; i < 2; i++ {
		frontHash := fmt.Sprintf("url%d", i)
		tailHash := fmt.Sprintf("url%d_tail", i)

		if _, err := os.Stat(filepath.Join(fc.dir, frontHash)); err == nil {
			t.Errorf("Front file %s should have been evicted", frontHash)
		}
		if _, err := os.Stat(filepath.Join(fc.dir, tailHash)); err == nil {
			t.Errorf("Tail file %s should have been evicted", tailHash)
		}

		var exists bool
		if err := fc.db.QueryRow("SELECT 1 FROM cache_files WHERE hash = ?", frontHash).Scan(&exists); err == nil {
			t.Errorf("Front entry %s should have been evicted from DB", frontHash)
		}
		if err := fc.db.QueryRow("SELECT 1 FROM cache_files WHERE hash = ?", tailHash).Scan(&exists); err == nil {
			t.Errorf("Tail entry %s should have been evicted from DB", tailHash)
		}
	}

	// URLs 2-6 should still exist (both front and tail)
	for i := 2; i < 7; i++ {
		frontHash := fmt.Sprintf("url%d", i)
		tailHash := fmt.Sprintf("url%d_tail", i)

		if _, err := os.Stat(filepath.Join(fc.dir, frontHash)); err != nil {
			t.Errorf("Front file %s should still exist", frontHash)
		}
		if _, err := os.Stat(filepath.Join(fc.dir, tailHash)); err != nil {
			t.Errorf("Tail file %s should still exist", tailHash)
		}
	}
}
