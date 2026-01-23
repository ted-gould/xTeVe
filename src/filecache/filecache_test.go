package filecache

import (
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
	// Create files exceeding the max (default is 100, so create 105)
	// We create them manually in the directory to simulate existing cache items
	// that will be picked up by loadCache.
	numFiles := maxItems + 5
	for i := 0; i < numFiles; i++ {
		hash := fmt.Sprintf("hash%d", i)
		path := filepath.Join(fc.dir, hash)
		metaPath := path + ".json"

		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(metaPath, []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Reload cache to populate items map
	Reset()
	fc, err = GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Manually "touch" items 5 to numFiles-1 to make them newer
	// items 0-4 will remain with the initial load timestamp (older)
	for i := 5; i < numFiles; i++ {
		hash := fmt.Sprintf("hash%d", i)
		fc.mutex.Lock()
		if item, ok := fc.items[hash]; ok {
			item.AccessTime = time.Now().Add(1 * time.Hour) // Future to ensure they are newer
		}
		fc.mutex.Unlock()
	}

	// Now trigger cleanup
	fc.CleanNow()

	// Check count
	fc.mutex.RLock()
	count := len(fc.items)
	fc.mutex.RUnlock()

	if count > maxItems {
		t.Errorf("Cache size %d exceeds max %d", count, maxItems)
	}

	// Verify items 0-4 are gone (Evicted)
	for i := 0; i < 5; i++ {
		hash := fmt.Sprintf("hash%d", i)
		path := filepath.Join(fc.dir, hash)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("Item %s should have been evicted", hash)
		}

		fc.mutex.RLock()
		_, ok := fc.items[hash]
		fc.mutex.RUnlock()
		if ok {
			t.Errorf("Item %s should have been removed from map", hash)
		}
	}

	// Verify item 5 is present
	hash := fmt.Sprintf("hash%d", 5)
	path := filepath.Join(fc.dir, hash)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Item %s should be present", hash)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore original env var
			oldVal := os.Getenv("WEBDAV_CACHE_SIZE")
			defer func() {
				if oldVal != "" {
					os.Setenv("WEBDAV_CACHE_SIZE", oldVal)
				} else {
					os.Unsetenv("WEBDAV_CACHE_SIZE")
				}
			}()

			// Set up test env var
			if tt.shouldUseEnv {
				os.Setenv("WEBDAV_CACHE_SIZE", tt.envValue)
			} else {
				os.Unsetenv("WEBDAV_CACHE_SIZE")
			}

			// Test getMaxCacheItems
			got := getMaxCacheItems()
			if got != tt.expectedSize {
				t.Errorf("getMaxCacheItems() = %d, want %d", got, tt.expectedSize)
			}
		})
	}
}

func TestConfigurableCacheSizeIntegration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filecache_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Set custom cache size
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
	// Create 30 files (max is 25)
	for i := 0; i < 30; i++ {
		hash := fmt.Sprintf("hash%d", i)
		path := filepath.Join(fc.dir, hash)
		metaPath := path + ".json"

		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(metaPath, []byte(`{}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Reload cache
	Reset()
	fc, err = GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Touch items 5-29 to make them newer
	for i := 5; i < 30; i++ {
		hash := fmt.Sprintf("hash%d", i)
		fc.mutex.Lock()
		if item, ok := fc.items[hash]; ok {
			item.AccessTime = time.Now().Add(1 * time.Hour)
		}
		fc.mutex.Unlock()
	}

	// Trigger cleanup
	fc.CleanNow()

	// Verify cache size respects custom limit
	fc.mutex.RLock()
	count := len(fc.items)
	fc.mutex.RUnlock()

	if count != customMax {
		t.Errorf("Cache size = %d, want %d", count, customMax)
	}

	// Verify oldest items (0-4) were evicted
	for i := 0; i < 5; i++ {
		hash := fmt.Sprintf("hash%d", i)
		fc.mutex.RLock()
		_, ok := fc.items[hash]
		fc.mutex.RUnlock()
		if ok {
			t.Errorf("Item %s should have been evicted", hash)
		}
	}

	// Verify newer items are still present
	hash := fmt.Sprintf("hash%d", 10)
	fc.mutex.RLock()
	_, ok := fc.items[hash]
	fc.mutex.RUnlock()
	if !ok {
		t.Errorf("Item %s should still be present", hash)
	}
}
