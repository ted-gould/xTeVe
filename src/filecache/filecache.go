package filecache

import (
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	DefaultMaxCacheItems = 100
	MaxCacheItems        = 100000      // Maximum allowed cache size
	MaxFileSize          = 1024 * 1024 // 1MB
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

type FileCache struct {
	dir   string
	db    *sql.DB
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

	dbPath := filepath.Join(cacheDir, "cache.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, err
	}

	// Create tables
	// cached_at is added to metadata
	query := `
	CREATE TABLE IF NOT EXISTS metadata (
		hash TEXT PRIMARY KEY,
		url TEXT,
		size INTEGER,
		mod_time INTEGER,
		etag TEXT,
		content_type TEXT,
		cached_at INTEGER
	);
	CREATE TABLE IF NOT EXISTS cache_files (
		hash TEXT PRIMARY KEY,
		cached_at INTEGER,
		complete BOOLEAN,
		access_time INTEGER,
		FOREIGN KEY(hash) REFERENCES metadata(hash)
	);
	`
	if _, err := db.Exec(query); err != nil {
		db.Close()
		return nil, err
	}

	instance = &FileCache{
		dir: cacheDir,
		db:  db,
	}

	instance.migrateFromJSON()
	go instance.cleaner()

	return instance, nil
}

// Reset clears the singleton instance. For testing purposes only.
func Reset() {
	initMu.Lock()
	defer initMu.Unlock()
	if instance != nil {
		instance.db.Close()
		instance = nil
	}
}

func (c *FileCache) migrateFromJSON() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Check if migration is needed (metadata table empty)
	var count int
	err := c.db.QueryRow("SELECT COUNT(*) FROM metadata").Scan(&count)
	if err != nil || count > 0 {
		return
	}

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}

		hash := e.Name()[:len(e.Name())-5] // Remove .json
		metaPath := filepath.Join(c.dir, e.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}

		var meta Metadata
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}

		// Insert metadata
		// Handle ModTime zero -> NULL
		var modTimeVal interface{}
		if !meta.ModTime.IsZero() {
			modTimeVal = meta.ModTime.UnixNano()
		} else {
			modTimeVal = nil
		}

		_, err = c.db.Exec(`INSERT OR REPLACE INTO metadata (hash, url, size, mod_time, etag, content_type, cached_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			hash, meta.URL, meta.Size, modTimeVal, meta.ETag, meta.ContentType, meta.CachedAt.UnixNano())
		if err != nil {
			continue
		}

		// Check content file
		contentPath := filepath.Join(c.dir, hash)
		if info, err := os.Stat(contentPath); err == nil {
			// Insert cache_files
			accessTime := time.Now().UnixNano()
			cachedAt := meta.CachedAt.UnixNano()
			if meta.CachedAt.IsZero() {
				cachedAt = info.ModTime().UnixNano()
			}

			_, err = c.db.Exec(`INSERT OR REPLACE INTO cache_files (hash, cached_at, complete, access_time) VALUES (?, ?, ?, ?)`,
				hash, cachedAt, meta.Complete, accessTime)
			if err != nil {
				continue
			}
		}

		// Delete JSON file
		os.Remove(metaPath)
	}
}

func HashURL(url string) string {
	hash := md5.Sum([]byte(url))
	return hex.EncodeToString(hash[:])
}

// getMaxCacheItems returns the configured maximum cache items, defaulting to DefaultMaxCacheItems.
func getMaxCacheItems() int {
	if val := os.Getenv("WEBDAV_CACHE_SIZE"); val != "" {
		if size, err := strconv.Atoi(val); err == nil && size > 0 {
			if size > MaxCacheItems {
				return MaxCacheItems
			}
			return size
		}
	}
	return DefaultMaxCacheItems
}

// Get returns the path to the cached file and its metadata if available.
func (c *FileCache) Get(url string) (string, *Metadata, bool) {
	hash := HashURL(url)
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var meta Metadata
	var modTimeNano sql.NullInt64
	var cachedAtNano, metaCachedAtNano int64
	var complete bool

	query := `
		SELECT m.url, m.size, m.mod_time, m.etag, m.content_type, m.cached_at, c.cached_at, c.complete
		FROM cache_files c
		JOIN metadata m ON c.hash = m.hash
		WHERE c.hash = ?
	`

	err := c.db.QueryRow(query, hash).Scan(
		&meta.URL, &meta.Size, &modTimeNano, &meta.ETag, &meta.ContentType, &metaCachedAtNano, &cachedAtNano, &complete,
	)
	if err != nil {
		return "", nil, false
	}

	if modTimeNano.Valid {
		meta.ModTime = time.Unix(0, modTimeNano.Int64)
	}
	// else ModTime is zero value

	meta.CachedAt = time.Unix(0, cachedAtNano)
	meta.Complete = complete

	// Verify file exists on disk
	path := filepath.Join(c.dir, hash)
	if _, err := os.Stat(path); err != nil {
		// File missing, remove from cache_files
		c.db.Exec("DELETE FROM cache_files WHERE hash = ?", hash)
		return "", nil, false
	}

	// Update Access Time
	nowNano := time.Now().UnixNano()
	c.db.Exec("UPDATE cache_files SET access_time = ? WHERE hash = ?", nowNano, hash)

	return path, &meta, true
}

// GetMetadata returns the metadata and file info of the cached file (or sidecar), even if content is missing.
func (c *FileCache) GetMetadata(url string) (*Metadata, os.FileInfo, bool) {
	hash := HashURL(url)
	c.mutex.Lock()
	defer c.mutex.Unlock()

	var meta Metadata
	var modTimeNano sql.NullInt64
	var cachedAtNano int64

	// Select cached_at from metadata
	err := c.db.QueryRow(`SELECT url, size, mod_time, etag, content_type, cached_at FROM metadata WHERE hash = ?`, hash).Scan(
		&meta.URL, &meta.Size, &modTimeNano, &meta.ETag, &meta.ContentType, &cachedAtNano,
	)
	if err != nil {
		return nil, nil, false
	}

	if modTimeNano.Valid {
		meta.ModTime = time.Unix(0, modTimeNano.Int64)
	}

	meta.CachedAt = time.Unix(0, cachedAtNano)

	return &meta, nil, true
}

// WriteMetadata writes the metadata to the DB.
func (c *FileCache) WriteMetadata(url string, meta Metadata) error {
	hash := HashURL(url)
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Ensure cached_at is set
	cachedAt := meta.CachedAt.UnixNano()
	if meta.CachedAt.IsZero() {
		cachedAt = time.Now().UnixNano()
	}

	var modTimeVal interface{}
	if !meta.ModTime.IsZero() {
		modTimeVal = meta.ModTime.UnixNano()
	} else {
		modTimeVal = nil
	}

	_, err := c.db.Exec(`INSERT OR REPLACE INTO metadata (hash, url, size, mod_time, etag, content_type, cached_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		hash, url, meta.Size, modTimeVal, meta.ETag, meta.ContentType, cachedAt)
	return err
}

// StartCaching triggers a background download if the file is not already cached.
func (c *FileCache) StartCaching(url string, client *http.Client, userAgent string) {
	hash := HashURL(url)

	c.mutex.RLock()
	var exists bool
	// Check if it exists in cache_files and is complete
	err := c.db.QueryRow("SELECT 1 FROM cache_files WHERE hash = ? AND complete = 1", hash).Scan(&exists)
	c.mutex.RUnlock()

	if err == nil && exists {
		return
	}

	go c.download(url, hash, client, userAgent)
}

func (c *FileCache) download(urlStr, hash string, client *http.Client, userAgent string) {
	// Create temp file
	tmpFile, err := os.CreateTemp(c.dir, "tmp_*")
	if err != nil {
		return
	}
	defer os.Remove(tmpFile.Name())

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

	_, err = io.CopyN(tmpFile, resp.Body, MaxFileSize)
	complete := false
	if err == nil {
		// Copied exactly MaxFileSize.
	} else if err == io.EOF {
		complete = true
	} else {
		tmpFile.Close()
		return
	}
	tmpFile.Close()

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

	finalPath := filepath.Join(c.dir, hash)
	if err := os.Rename(tmpFile.Name(), finalPath); err != nil {
		return
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	var modTimeVal interface{}
	if !meta.ModTime.IsZero() {
		modTimeVal = meta.ModTime.UnixNano()
	} else {
		modTimeVal = nil
	}

	// Update metadata with cached_at
	c.db.Exec(`INSERT OR REPLACE INTO metadata (hash, url, size, mod_time, etag, content_type, cached_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		hash, meta.URL, meta.Size, modTimeVal, meta.ETag, meta.ContentType, meta.CachedAt.UnixNano())

	// Update cache_files
	c.db.Exec(`INSERT OR REPLACE INTO cache_files (hash, cached_at, complete, access_time) VALUES (?, ?, ?, ?)`,
		hash, meta.CachedAt.UnixNano(), meta.Complete, time.Now().UnixNano())
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

	maxItems := getMaxCacheItems()

	// Count items
	var count int
	if err := c.db.QueryRow("SELECT COUNT(*) FROM cache_files").Scan(&count); err != nil {
		return
	}

	if count <= maxItems {
		return
	}

	toRemove := count - maxItems
	rows, err := c.db.Query("SELECT hash FROM cache_files ORDER BY access_time ASC LIMIT ?", toRemove)
	if err != nil {
		return
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err == nil {
			hashes = append(hashes, h)
		}
	}
	rows.Close()

	for _, h := range hashes {
		os.Remove(filepath.Join(c.dir, h))
		c.db.Exec("DELETE FROM cache_files WHERE hash = ?", h)
	}
}

// RemoveAll clears the cache (useful for testing)
func (c *FileCache) RemoveAll() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.db.Close()
	os.RemoveAll(c.dir)
	os.MkdirAll(c.dir, 0755)

	// Re-init DB
	dbPath := filepath.Join(c.dir, "cache.db")
	db, _ := sql.Open("sqlite", dbPath)
	db.Exec("PRAGMA journal_mode=WAL;")
	c.db = db

	// Create tables again
	query := `
	CREATE TABLE IF NOT EXISTS metadata (
		hash TEXT PRIMARY KEY,
		url TEXT,
		size INTEGER,
		mod_time INTEGER,
		etag TEXT,
		content_type TEXT,
		cached_at INTEGER
	);
	CREATE TABLE IF NOT EXISTS cache_files (
		hash TEXT PRIMARY KEY,
		cached_at INTEGER,
		complete BOOLEAN,
		access_time INTEGER,
		FOREIGN KEY(hash) REFERENCES metadata(hash)
	);
	`
	c.db.Exec(query)
}
