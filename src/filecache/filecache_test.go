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

	Reset()
	fc, err := GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create 130 files (Max is 128)
	// We create them manually in the directory to simulate existing cache items
	// that will be picked up by loadCache.
	for i := 0; i < MaxCacheItems+5; i++ {
		hash := fmt.Sprintf("hash%d", i)
		path := filepath.Join(fc.dir, hash)
		metaPath := path + ".json"

		os.WriteFile(path, []byte("data"), 0644)
		os.WriteFile(metaPath, []byte(`{}`), 0644)
	}

	// Reload cache to populate items map
	Reset()
	fc, err = GetInstance(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Manually "touch" items 5 to 132 to make them newer
	// items 0-4 will remain with the initial load timestamp (older)
	for i := 5; i < MaxCacheItems+5; i++ {
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

	if count > MaxCacheItems {
		t.Errorf("Cache size %d exceeds max %d", count, MaxCacheItems)
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
