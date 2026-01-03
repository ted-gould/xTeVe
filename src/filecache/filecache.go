package filecache

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

const (
	MaxCacheItems = 128
	MaxFileSize   = 1024 * 1024 // 1MB
)

type Metadata struct {
	URL         string    `json:"url"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
	ETag        string    `json:"etag"`
	ContentType string    `json:"content_type"`
	CachedAt    time.Time `json:"cached_at"`
	Complete    bool      `json:"complete"`
}

type CacheItem struct {
	Hash       string
	Path       string
	MetaPath   string
	AccessTime time.Time
}

type FileCache struct {
	dir   string
	items map[string]*CacheItem
	mutex sync.RWMutex
}

var (
	instance *FileCache
	initMu   sync.Mutex
)

// GetInstance returns the singleton instance, initializing it if necessary
func GetInstance(baseDir string) (*FileCache, error) {
	initMu.Lock()
	defer initMu.Unlock()

	if instance != nil {
		return instance, nil
	}

	// Use SNAP_COMMON if available, otherwise use baseDir/xteve_cache
	cacheDir := os.Getenv("SNAP_COMMON")
	if cacheDir != "" {
		cacheDir = filepath.Join(cacheDir, "xteve_cache")
	} else {
		cacheDir = filepath.Join(baseDir, "xteve_cache")
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	instance = &FileCache{
		dir:   cacheDir,
		items: make(map[string]*CacheItem),
	}
	instance.loadCache()
	go instance.cleaner()

	return instance, nil
}

// Reset clears the singleton instance. For testing purposes only.
func Reset() {
	initMu.Lock()
	defer initMu.Unlock()
	instance = nil
}

func (c *FileCache) loadCache() {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) == ".json" {
			continue
		}
		hash := e.Name()
		metaPath := filepath.Join(c.dir, hash+".json")
		if _, err := os.Stat(metaPath); err == nil {
			c.items[hash] = &CacheItem{
				Hash:       hash,
				Path:       filepath.Join(c.dir, hash),
				MetaPath:   metaPath,
				AccessTime: time.Now(),
			}
		}
	}
}

func HashURL(url string) string {
	hash := md5.Sum([]byte(url))
	return hex.EncodeToString(hash[:])
}

// Get returns the path to the cached file and its metadata if available.
func (c *FileCache) Get(url string) (string, *Metadata, bool) {
	hash := HashURL(url)
	c.mutex.Lock()
	defer c.mutex.Unlock()

	item, ok := c.items[hash]
	if !ok {
		return "", nil, false
	}

	item.AccessTime = time.Now()

	data, err := os.ReadFile(item.MetaPath)
	if err != nil {
		return "", nil, false
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", nil, false
	}

	return item.Path, &meta, true
}

// StartCaching triggers a background download if the file is not already cached.
func (c *FileCache) StartCaching(url string, client *http.Client, userAgent string) {
	hash := HashURL(url)

	c.mutex.RLock()
	// Check if already cached
	if _, ok := c.items[hash]; ok {
		c.mutex.RUnlock()
		return
	}
	c.mutex.RUnlock()

	// Double check if file exists on disk (maybe created by another process or raced)
	// Actually StartCaching is called when Get failed, but there's a race between Get and StartCaching.
	// We'll just trust that if it's not in items map, we should try to cache it.
	// We can use a "pending" map if needed, but simple go routine is likely fine.

	go c.download(url, hash, client, userAgent)
}

func (c *FileCache) download(urlStr, hash string, client *http.Client, userAgent string) {
	// Prevent concurrent downloads for the same hash could be done with a map of mutexes,
	// but for now relying on chance and overwrites is acceptable for "simple" cache.

	// Create temp file
	tmpFile, err := os.CreateTemp(c.dir, "tmp_*")
	if err != nil {
		return
	}
	defer os.Remove(tmpFile.Name()) // Clean up if not renamed

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		tmpFile.Close()
		return
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	resp, err := client.Do(req)
	if err != nil {
		tmpFile.Close()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return
	}

	// Read up to MaxFileSize
	_, err = io.CopyN(tmpFile, resp.Body, MaxFileSize)
	complete := false
	if err == nil {
		// Copied exactly MaxFileSize. Assumed incomplete.
	} else if err == io.EOF {
		complete = true
	} else {
		tmpFile.Close()
		return
	}

	tmpFile.Close()

	// Prepare metadata
	meta := Metadata{
		URL:         urlStr,
		Size:        resp.ContentLength,
		ModTime:     time.Now(),
		ContentType: resp.Header.Get("Content-Type"),
		ETag:        resp.Header.Get("ETag"),
		CachedAt:    time.Now(),
		Complete:    complete,
	}
	if lastMod := resp.Header.Get("Last-Modified"); lastMod != "" {
		if t, err := http.ParseTime(lastMod); err == nil {
			meta.ModTime = t
		}
	}

	// Write metadata
	metaPath := filepath.Join(c.dir, hash+".json")
	metaData, err := json.Marshal(meta)
	if err != nil {
		return
	}
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return
	}

	// Move file
	finalPath := filepath.Join(c.dir, hash)
	// Rename might fail if across devices, but we created temp in same dir
	if err := os.Rename(tmpFile.Name(), finalPath); err != nil {
		// Try copy if rename fails?
		return
	}

	// Update items
	c.mutex.Lock()
	c.items[hash] = &CacheItem{
		Hash:       hash,
		Path:       finalPath,
		MetaPath:   metaPath,
		AccessTime: time.Now(),
	}
	c.mutex.Unlock()
}

func (c *FileCache) cleaner() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		c.CleanNow()
	}
}

// CleanNow performs the cleanup immediately.
func (c *FileCache) CleanNow() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if len(c.items) <= MaxCacheItems {
		return
	}

	type entry struct {
		hash string
		time time.Time
	}
	var entries []entry
	for k, v := range c.items {
		entries = append(entries, entry{hash: k, time: v.AccessTime})
	}

	slices.SortFunc(entries, func(a, b entry) int {
		return a.time.Compare(b.time)
	})

	toRemove := len(entries) - MaxCacheItems
	for i := 0; i < toRemove; i++ {
		e := entries[i]
		item := c.items[e.hash]
		os.Remove(item.Path)
		os.Remove(item.MetaPath)
		delete(c.items, e.hash)
	}
}

// RemoveAll clears the cache (useful for testing)
func (c *FileCache) RemoveAll() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	_ = os.RemoveAll(c.dir)
	_ = os.MkdirAll(c.dir, 0755)
	c.items = make(map[string]*CacheItem)
}
