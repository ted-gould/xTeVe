package filecache

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const (
	MaxCacheItems = 128
	MaxCacheSize  = 1024 * 1024 // 1MB
)

type CacheManager struct {
	cacheDir string
	mutex    sync.RWMutex
}

type Metadata struct {
	Size         int64
	ModTime      time.Time
	ContentType  string
	ResponseCode int
	URL          string
}

// NewCacheManager creates a new cache manager.
// If cacheDir is empty, it attempts to use SNAP_COMMON or a temp directory.
func NewCacheManager(cacheDir string) (*CacheManager, error) {
	if cacheDir == "" {
		if snapCommon := os.Getenv("SNAP_COMMON"); snapCommon != "" {
			cacheDir = filepath.Join(snapCommon, "xteve_cache")
		} else {
			cacheDir = filepath.Join(os.TempDir(), "xteve_cache")
		}
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	cm := &CacheManager{
		cacheDir: cacheDir,
	}

	// Start background eviction task
	go cm.evictionLoop()

	return cm, nil
}

func (cm *CacheManager) hashURL(url string) string {
	h := md5.New()
	_, _ = io.WriteString(h, url) // hasher writes never fail
	return hex.EncodeToString(h.Sum(nil))
}

// GetPath returns the full path for a file with the given URL
func (cm *CacheManager) GetPath(url string) string {
	return filepath.Join(cm.cacheDir, cm.hashURL(url))
}

// Exists checks if a file and its metadata exist in the cache
func (cm *CacheManager) Exists(url string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	path := cm.GetPath(url)
	if _, err := os.Stat(path); err != nil {
		return false
	}
	if _, err := os.Stat(path + ".json"); err != nil {
		return false
	}
	return true
}

// GetMetadata retrieves metadata for a cached file
func (cm *CacheManager) GetMetadata(url string) (Metadata, error) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	var meta Metadata
	path := cm.GetPath(url)
	data, err := os.ReadFile(path + ".json")
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(data, &meta)
	// Update access time for LRU
	if err == nil {
		now := time.Now()
		_ = os.Chtimes(path, now, now) // Best effort
	}
	return meta, err
}

// Get returns a ReadCloser for the cached file.
// If the file was only partially cached (1MB limit), the caller is responsible
// for handling that (the Metadata.Size will tell the *real* size vs file size).
func (cm *CacheManager) Get(url string) (io.ReadCloser, Metadata, error) {
	meta, err := cm.GetMetadata(url)
	if err != nil {
		return nil, meta, err
	}

	f, err := os.Open(cm.GetPath(url))
	if err != nil {
		return nil, meta, err
	}

	return f, meta, nil
}

// CacheReader wraps the download/cache process.
// It reads from the upstream response, writes to the cache file (up to 1MB),
// and satisfies the caller's read requests.
type CacheReader struct {
	ctx          context.Context
	upstream     io.ReadCloser
	cacheFile    *os.File
	teeReader    io.Reader
	metadata     Metadata
	url          string
	cm           *CacheManager
	closeOnce    sync.Once
	// If the caller closes early, we might detach and keep downloading
	detachChan   chan struct{}
}

// NewCacheReader creates a reader that reads from upstream and tees to cache.
// It also writes metadata on creation (or completion).
func (cm *CacheManager) NewCacheReader(ctx context.Context, url string, upstream io.ReadCloser, meta Metadata) (*CacheReader, error) {
	path := cm.GetPath(url)
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	// Write initial metadata. We might update it later if needed.
	if err := cm.writeMetadata(url, meta); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}

	cr := &CacheReader{
		ctx:        ctx,
		upstream:   upstream,
		cacheFile:  f,
		metadata:   meta,
		url:        url,
		cm:         cm,
		detachChan: make(chan struct{}),
	}

	// We wrap the upstream in a TeeReader.
	// However, we need to stop writing to cache after MaxCacheSize.
	// But we must continue serving the caller.
	cr.teeReader = &limitWriterTeeReader{
		r:     upstream,
		w:     f,
		limit: MaxCacheSize,
	}

	return cr, nil
}

// limitWriterTeeReader works like io.TeeReader but stops writing to w after limit is reached,
// while continuing to read from r.
type limitWriterTeeReader struct {
	r       io.Reader
	w       io.Writer
	limit   int64
	written int64
}

func (t *limitWriterTeeReader) Read(p []byte) (n int, err error) {
	n, err = t.r.Read(p)
	if n > 0 {
		if t.written < t.limit {
			toWrite := int64(n)
			if t.written+toWrite > t.limit {
				toWrite = t.limit - t.written
			}
			// Write to cache, ignore errors to not break the stream for the user
			if wn, werr := t.w.Write(p[:toWrite]); werr == nil {
				t.written += int64(wn)
			}
		}
	}
	return
}

func (cr *CacheReader) Read(p []byte) (n int, err error) {
	return cr.teeReader.Read(p)
}

func (cr *CacheReader) Close() error {
	var err error
	cr.closeOnce.Do(func() {
		// If we haven't cached 1MB yet, and we haven't reached EOF, we should detach
		// and let a background goroutine finish the download up to 1MB.

		// Note: The TeeReader internally tracks written bytes.
		// We need to check if we should keep going.

		// Ideally, we can't easily peek into the TeeReader struct above unless we expose fields.
		// But we know if we haven't reached MaxCacheSize.

		lw := cr.teeReader.(*limitWriterTeeReader)

		if lw.written < lw.limit && lw.written < cr.metadata.Size {
			// Detach!
			go func() {
				// Create a buffer to drain the remaining needed bytes
				// We need to read (MaxCacheSize - lw.written) bytes
				remaining := lw.limit - lw.written
				if remaining > 0 {
					// We just read into discard, the TeeReader will write to the file
					_, _ = io.CopyN(io.Discard, cr.teeReader, remaining) // Best effort background download
				}
				cr.cacheFile.Close()
				cr.upstream.Close()
			}()
			return
		}

		// Normal close
		if cerr := cr.cacheFile.Close(); cerr != nil {
			err = cerr
		}
		if uerr := cr.upstream.Close(); uerr != nil && err == nil {
			err = uerr
		}
	})
	return err
}

func (cm *CacheManager) writeMetadata(url string, meta Metadata) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	path := cm.GetPath(url)
	return os.WriteFile(path + ".json", data, 0644)
}

func (cm *CacheManager) evictionLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		cm.Evict()
	}
}

func (cm *CacheManager) Evict() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return
	}

	type fileInfo struct {
		name    string
		modTime time.Time
	}
	var files []fileInfo

	for _, e := range entries {
		// We only track the data files, not the .json sidecars directly
		if filepath.Ext(e.Name()) == ".json" {
			continue
		}
		info, err := e.Info()
		if err == nil {
			files = append(files, fileInfo{name: e.Name(), modTime: info.ModTime()})
		}
	}

	if len(files) <= MaxCacheItems {
		return
	}

	// Sort by modTime (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	// Delete oldest until we are under the limit
	toDelete := len(files) - MaxCacheItems
	for i := 0; i < toDelete; i++ {
		os.Remove(filepath.Join(cm.cacheDir, files[i].name))
		os.Remove(filepath.Join(cm.cacheDir, files[i].name+".json"))
	}
}

// BackgroundDownload downloads the first 1MB of the URL to the cache.
// It is intended to be called in a goroutine.
func (cm *CacheManager) BackgroundDownload(ctx context.Context, url string, userAgent string) {
	// Check if exists first to avoid redundant work
	if cm.Exists(url) {
		return
	}

	_, span := otel.Tracer("webdav").Start(ctx, "BackgroundDownload")
	defer span.End()
	span.SetAttributes(attribute.String("url", url))

	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil) // Detached context
	if err != nil {
		return
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	meta := Metadata{
		Size:         resp.ContentLength,
		ModTime:      time.Now(),
		ContentType:  resp.Header.Get("Content-Type"),
		ResponseCode: resp.StatusCode,
		URL:          url,
	}
	if lastMod := resp.Header.Get("Last-Modified"); lastMod != "" {
		if t, err := http.ParseTime(lastMod); err == nil {
			meta.ModTime = t
		}
	}

	path := cm.GetPath(url)
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()

	if err := cm.writeMetadata(url, meta); err != nil {
		return
	}

	// Read up to MaxCacheSize
	_, _ = io.CopyN(f, resp.Body, MaxCacheSize) // Best effort to fill cache
}
