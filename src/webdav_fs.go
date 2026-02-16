package src

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/net/webdav"

	"xteve/src/filecache"
)

var (
	globalFileCache     *filecache.FileCache
	globalFileCacheOnce sync.Once
)

func getFileCache() *filecache.FileCache {
	globalFileCacheOnce.Do(func() {
		dir := System.Folder.Cache
		if dir == "" {
			dir = System.Folder.Temp
		}
		c, _ := filecache.GetInstance(dir)
		globalFileCache = c
	})
	return globalFileCache
}

const (
	dirOnDemand   = "On Demand"
	fileListing   = "listing.m3u"
	dirSeries     = "Series"
	dirIndividual = "Individual"
)

// WebDAVFS implements the webdav.FileSystem interface
type WebDAVFS struct {
}

// mkDirInfo implements os.FileInfo for a directory
type mkDirInfo struct {
	name    string
	modTime time.Time
}

func (d *mkDirInfo) Name() string       { return d.name }
func (d *mkDirInfo) Size() int64        { return 0 }
func (d *mkDirInfo) Mode() os.FileMode  { return os.ModeDir | 0555 }
func (d *mkDirInfo) ModTime() time.Time { return d.modTime }
func (d *mkDirInfo) IsDir() bool        { return true }
func (d *mkDirInfo) Sys() interface{}   { return nil }

// mkFileInfo implements os.FileInfo for a file
type mkFileInfo struct {
	name    string
	size    int64
	modTime time.Time
}

func (f *mkFileInfo) Name() string       { return f.name }
func (f *mkFileInfo) Size() int64        { return f.size }
func (f *mkFileInfo) Mode() os.FileMode  { return 0444 }
func (f *mkFileInfo) ModTime() time.Time { return f.modTime }
func (f *mkFileInfo) IsDir() bool        { return false }
func (f *mkFileInfo) Sys() interface{}   { return nil }

// Mkdir returns an error as the filesystem is read-only
func (fs *WebDAVFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	_, span := otel.Tracer("webdav").Start(ctx, "Mkdir")
	defer span.End()
	span.SetAttributes(attribute.String("webdav.path", name))
	span.RecordError(os.ErrPermission)
	return os.ErrPermission
}

// OpenFile opens a file or directory
func (fs *WebDAVFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (f webdav.File, err error) {
	ctx, span := otel.Tracer("webdav").Start(ctx, "OpenFile")
	defer span.End()

	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()

	span.SetAttributes(attribute.String("webdav.path", name))

	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, "/")

	// Root directory
	if name == "" {
		return &webdavDir{name: "", ctx: ctx}, nil
	}

	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return nil, os.ErrNotExist
	}

	hash := parts[0]
	if _, ok := Settings.Files.M3U[hash]; !ok {
		return nil, os.ErrNotExist
	}

	switch len(parts) {
	case 1:
		return fs.openHashDir(ctx, hash)
	case 2:
		return fs.openHashSubDir(ctx, hash, parts[1])
	case 3:
		return fs.openOnDemandGroupDir(ctx, hash, parts[1], parts[2])
	case 4:
		return fs.openOnDemandGroupSubDir(ctx, hash, parts[1], parts[2], parts[3])
	case 5:
		if parts[3] == dirSeries {
			return fs.openOnDemandSeriesDir(ctx, hash, parts[1], parts[2], parts[4])
		}
		if parts[3] == dirIndividual {
			return fs.openOnDemandIndividualStream(ctx, hash, parts[1], parts[2], parts[4])
		}
	case 6:
		if parts[3] == dirSeries {
			return fs.openOnDemandSeasonDir(ctx, hash, parts[1], parts[2], parts[4], parts[5])
		}
	case 7:
		if parts[3] == dirSeries {
			return fs.openOnDemandSeriesStream(ctx, hash, parts[1], parts[2], parts[4], parts[5], parts[6])
		}
	}

	return nil, os.ErrNotExist
}

func (fs *WebDAVFS) openHashDir(ctx context.Context, hash string) (webdav.File, error) {
	return &webdavDir{name: hash, ctx: ctx}, nil
}

func (fs *WebDAVFS) openHashSubDir(ctx context.Context, hash, sub string) (webdav.File, error) {
	if sub == fileListing {
		realPath := filepath.Join(System.Folder.Data, hash+".m3u")
		f, err := os.Open(realPath)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	if sub == dirOnDemand {
		return &webdavDir{name: path.Join(hash, sub), ctx: ctx}, nil
	}
	return nil, os.ErrNotExist
}

func (fs *WebDAVFS) groupExists(ctx context.Context, hash, group string) bool {
	groups := getGroupsForHash(ctx, hash)
	return slices.ContainsFunc(groups, func(g string) bool {
		return sanitizeGroupName(g) == group
	})
}

func (fs *WebDAVFS) openOnDemandGroupDir(ctx context.Context, hash, sub, group string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	if !fs.groupExists(ctx, hash, group) {
		return nil, os.ErrNotExist
	}
	return &webdavDir{name: path.Join(hash, sub, group), ctx: ctx}, nil
}

func (fs *WebDAVFS) openOnDemandGroupSubDir(ctx context.Context, hash, sub, group, typeDir string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	if !fs.groupExists(ctx, hash, group) {
		return nil, os.ErrNotExist
	}
	if typeDir == dirSeries || typeDir == dirIndividual {
		return &webdavDir{name: path.Join(hash, sub, group, typeDir), ctx: ctx}, nil
	}
	return nil, os.ErrNotExist
}

func (fs *WebDAVFS) openOnDemandSeriesDir(ctx context.Context, hash, sub, group, series string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	if !fs.groupExists(ctx, hash, group) {
		return nil, os.ErrNotExist
	}
	// Check if series exists
	if !slices.Contains(getSeriesList(ctx, hash, group), series) {
		return nil, os.ErrNotExist
	}
	return &webdavDir{name: path.Join(hash, sub, group, dirSeries, series), ctx: ctx}, nil
}

func (fs *WebDAVFS) openOnDemandSeasonDir(ctx context.Context, hash, sub, group, series, season string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	if !fs.groupExists(ctx, hash, group) {
		return nil, os.ErrNotExist
	}
	// Check if season exists
	if !slices.Contains(getSeasonsList(ctx, hash, group, series), season) {
		return nil, os.ErrNotExist
	}
	return &webdavDir{name: path.Join(hash, sub, group, dirSeries, series, season), ctx: ctx}, nil
}

func (fs *WebDAVFS) openOnDemandIndividualStream(ctx context.Context, hash, sub, group, filename string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	stream, targetURL, err := findIndividualStream(ctx, hash, group, filename)
	if err != nil {
		return nil, os.ErrNotExist
	}
	modTime := getM3UModTime(hash)
	return &webdavStream{
		stream:    stream,
		name:      filename,
		ctx:       ctx,
		modTime:   modTime,
		targetURL: targetURL,
		hash:      hash,
	}, nil
}

func (fs *WebDAVFS) openOnDemandSeriesStream(ctx context.Context, hash, sub, group, series, season, filename string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	stream, targetURL, err := findSeriesStream(ctx, hash, group, series, season, filename)
	if err != nil {
		return nil, os.ErrNotExist
	}
	modTime := getM3UModTime(hash)
	return &webdavStream{
		stream:    stream,
		name:      filename,
		ctx:       ctx,
		modTime:   modTime,
		targetURL: targetURL,
		hash:      hash,
	}, nil
}

// RemoveAll returns an error as the filesystem is read-only
func (fs *WebDAVFS) RemoveAll(ctx context.Context, name string) error {
	ctx, span := otel.Tracer("webdav").Start(ctx, "RemoveAll")
	defer span.End()
	span.SetAttributes(attribute.String("webdav.path", name))

	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, "/")

	if name == "" {
		span.RecordError(os.ErrPermission)
		return os.ErrPermission
	}

	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		span.RecordError(os.ErrNotExist)
		return os.ErrNotExist
	}

	hash := parts[0]
	if _, ok := Settings.Files.M3U[hash]; !ok {
		span.RecordError(os.ErrNotExist)
		return os.ErrNotExist
	}

	// Just check if it exists and return ErrPermission, otherwise ErrNotExist
	_, err := fs.Stat(ctx, name)
	if err != nil {
		span.RecordError(os.ErrNotExist)
		return os.ErrNotExist
	}
	span.RecordError(os.ErrPermission)
	return os.ErrPermission
}

// Rename returns an error as the filesystem is read-only
func (fs *WebDAVFS) Rename(ctx context.Context, oldName, newName string) error {
	_, span := otel.Tracer("webdav").Start(ctx, "Rename")
	defer span.End()
	span.SetAttributes(
		attribute.String("webdav.old_path", oldName),
		attribute.String("webdav.new_path", newName),
	)
	span.RecordError(os.ErrPermission)
	return os.ErrPermission
}

// Stat returns file info
func (fs *WebDAVFS) Stat(ctx context.Context, name string) (_ os.FileInfo, err error) {
	_, span := otel.Tracer("webdav").Start(ctx, "Stat")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	span.SetAttributes(attribute.String("webdav.path", name))

	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, "/")

	if name == "" {
		return &mkDirInfo{name: "", modTime: time.Now()}, nil
	}

	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return nil, os.ErrNotExist
	}

	hash := parts[0]
	if _, ok := Settings.Files.M3U[hash]; !ok {
		return nil, os.ErrNotExist
	}

	modTime := getM3UModTime(hash)

	switch len(parts) {
	case 1:
		return fs.statHashDir(hash, modTime)
	case 2:
		return fs.statHashSubDir(hash, parts[1], modTime)
	case 3:
		// Group dir
		if parts[1] == dirOnDemand && fs.groupExists(ctx, hash, parts[2]) {
			return &mkDirInfo{name: parts[2], modTime: modTime}, nil
		}
	case 4:
		// Series or Individual dir
		if parts[1] == dirOnDemand && fs.groupExists(ctx, hash, parts[2]) {
			if parts[3] == dirSeries || parts[3] == dirIndividual {
				return &mkDirInfo{name: parts[3], modTime: modTime}, nil
			}
		}
	case 5:
		if parts[1] == dirOnDemand && fs.groupExists(ctx, hash, parts[2]) {
			if parts[3] == dirIndividual {
				// File in Individual
				stream, targetURL, err := findIndividualStream(ctx, hash, parts[2], parts[4])
				if err == nil {
					return fs.statWithMetadata(ctx, hash, stream, targetURL, parts[4], modTime)
				}
			} else if parts[3] == dirSeries {
				// Series Dir
				seriesList := getSeriesList(ctx, hash, parts[2])
				for _, s := range seriesList {
					if s == parts[4] {
						return &mkDirInfo{name: parts[4], modTime: modTime}, nil
					}
				}
			}
		}
	case 6:
		if parts[1] == dirOnDemand && fs.groupExists(ctx, hash, parts[2]) && parts[3] == dirSeries {
			// Season Dir
			seasons := getSeasonsList(ctx, hash, parts[2], parts[4])
			for _, s := range seasons {
				if s == parts[5] {
					return &mkDirInfo{name: parts[5], modTime: modTime}, nil
				}
			}
		}
	case 7:
		if parts[1] == dirOnDemand && fs.groupExists(ctx, hash, parts[2]) && parts[3] == dirSeries {
			// File in Series
			stream, targetURL, err := findSeriesStream(ctx, hash, parts[2], parts[4], parts[5], parts[6])
			if err == nil {
				return fs.statWithMetadata(ctx, hash, stream, targetURL, parts[6], modTime)
			}
		}
	}

	return nil, os.ErrNotExist
}

func (fs *WebDAVFS) statWithMetadata(ctx context.Context, hash string, stream map[string]string, targetURL, name string, defaultModTime time.Time) (os.FileInfo, error) {
	ctx, span := otel.Tracer("webdav").Start(ctx, "statWithMetadata")
	defer span.End()
	return resolveFileMetadata(ctx, hash, stream, targetURL, name, defaultModTime)
}

func resolveFileMetadata(ctx context.Context, hash string, stream map[string]string, targetURL, name string, defaultModTime time.Time) (os.FileInfo, error) {
	ctx, span := otel.Tracer("webdav").Start(ctx, "resolveFileMetadata")
	defer span.End()
	span.SetAttributes(attribute.String("target_url", targetURL))

	// 1. Try Cache
	if meta, found := resolveMetadataFromCache(hash, targetURL); found {
		span.SetAttributes(
			attribute.String("metadata.source", "cache"),
			attribute.Int64("file.size", meta.Size),
		)
		mt := defaultModTime
		if !meta.ModTime.IsZero() {
			mt = meta.ModTime
		}
		return &mkFileInfo{name: name, size: meta.Size, modTime: mt}, nil
	}

	// 2. Try M3U attributes (only if this is the video stream)
	if meta, found := resolveMetadataFromM3U(hash, stream, targetURL); found {
		mt := defaultModTime
		if !meta.ModTime.IsZero() {
			mt = meta.ModTime
		}
		if meta.Size > 0 {
			span.SetAttributes(
				attribute.String("metadata.source", "m3u"),
				attribute.Int64("file.size", meta.Size),
			)
			return &mkFileInfo{name: name, size: meta.Size, modTime: mt}, nil
		}
		// Update defaultModTime with what we found (if it's not zero), but continue to remote fetch for size
		if !mt.IsZero() {
			defaultModTime = mt
		}
	}

	// 3. Remote fetch
	if meta, err := resolveMetadataFromRemote(ctx, hash, targetURL); err == nil {
		span.SetAttributes(
			attribute.String("metadata.source", "remote"),
			attribute.Int64("file.size", meta.Size),
		)
		mt := defaultModTime
		if !meta.ModTime.IsZero() {
			mt = meta.ModTime
		}
		return &mkFileInfo{name: name, size: meta.Size, modTime: mt}, nil
	}

	span.SetAttributes(
		attribute.String("metadata.source", "none"),
		attribute.Int64("file.size", 0),
	)
	return &mkFileInfo{name: name, size: 0, modTime: defaultModTime}, nil
}

func resolveMetadataFromCache(hash, targetURL string) (FileMeta, bool) {
	webdavCacheMutex.RLock()
	defer webdavCacheMutex.RUnlock()

	hc := webdavCache[hash]
	if hc != nil && targetURL != "" {
		if meta, ok := hc.FileMetadata[targetURL]; ok {
			return meta, true
		}
	}
	return FileMeta{}, false
}

func resolveMetadataFromM3U(hash string, stream map[string]string, targetURL string) (FileMeta, bool) {
	// Only check M3U attributes if targetURL is the main stream URL
	isVideo := targetURL == stream["url"]
	if !isVideo {
		return FileMeta{}, false
	}

	meta, found := getStreamMetadata(stream)
	if found && meta.Size > 0 {
		updateMetadataCache(hash, targetURL, meta)
	}
	return meta, found
}

func resolveMetadataFromRemote(ctx context.Context, hash, targetURL string) (FileMeta, error) {
	meta, err := fetchRemoteMetadataFunc(ctx, targetURL)
	if err == nil {
		updateMetadataCache(hash, targetURL, meta)
	}
	return meta, err
}

func updateMetadataCache(hash, targetURL string, meta FileMeta) {
	webdavCacheMutex.Lock()
	defer webdavCacheMutex.Unlock()

	hc, ok := webdavCache[hash]
	if !ok {
		hc = &HashCache{
			Series:          make(map[string][]string),
			Seasons:         make(map[seasonKey][]string),
			SeasonFiles:     make(map[seasonFileKey][]FileStreamInfo),
			IndividualFiles: make(map[string][]FileStreamInfo),
			FileMetadata:    make(map[string]FileMeta),
		}
		webdavCache[hash] = hc
	}
	if hc.FileMetadata == nil {
		hc.FileMetadata = make(map[string]FileMeta)
	}
	hc.FileMetadata[targetURL] = meta
}

func (fs *WebDAVFS) statHashDir(hash string, modTime time.Time) (os.FileInfo, error) {
	return &mkDirInfo{name: hash, modTime: modTime}, nil
}

func (fs *WebDAVFS) statHashSubDir(hash, sub string, modTime time.Time) (os.FileInfo, error) {
	if sub == fileListing {
		// Use real file stat for listing.m3u
		realPath := filepath.Join(System.Folder.Data, hash+".m3u")
		info, err := os.Stat(realPath)
		if err != nil {
			return nil, err
		}
		return &mkFileInfo{name: fileListing, size: info.Size(), modTime: info.ModTime()}, nil
	}
	if sub == dirOnDemand {
		return &mkDirInfo{name: dirOnDemand, modTime: modTime}, nil
	}
	return nil, os.ErrNotExist
}

// webdavDir represents a virtual directory
type webdavDir struct {
	name string // Full virtual path relative to root
	pos  int
	ctx  context.Context
}

func (d *webdavDir) Close() (err error) {
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_, span := otel.Tracer("webdav").Start(ctx, "Close")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	span.SetAttributes(attribute.String("webdav.path", d.name))
	return nil
}

func (d *webdavDir) Read(p []byte) (n int, err error) {
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_, span := otel.Tracer("webdav").Start(ctx, "Read")
	defer span.End()
	span.SetAttributes(attribute.String("webdav.path", d.name))
	return 0, io.EOF
}

func (d *webdavDir) Seek(offset int64, whence int) (_ int64, err error) {
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_, span := otel.Tracer("webdav").Start(ctx, "Seek")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	span.SetAttributes(
		attribute.String("webdav.path", d.name),
		attribute.Int64("offset", offset),
		attribute.Int("whence", whence),
	)
	return 0, nil
}

func (d *webdavDir) Readdir(count int) ([]os.FileInfo, error) {
	// Use stored context if available, otherwise background
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	_, span := otel.Tracer("webdav").Start(ctx, "Readdir")
	defer span.End()
	span.SetAttributes(attribute.String("webdav.path", d.name))

	infos, err := d.collectInfos(ctx)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	if count > 0 {
		if d.pos >= len(infos) {
			return nil, io.EOF
		}
		end := d.pos + count
		if end > len(infos) {
			end = len(infos)
		}
		res := infos[d.pos:end]
		d.pos = end
		span.SetAttributes(attribute.Int("webdav.readdir.count", len(res)))
		return res, nil
	}

	span.SetAttributes(attribute.Int("webdav.readdir.count", len(infos)))
	return infos, nil
}

func (d *webdavDir) collectInfos(ctx context.Context) ([]os.FileInfo, error) {
	if d.name == "" {
		return d.readDirRoot()
	}

	parts := strings.Split(d.name, "/")

	// We can try to extract hash from parts[0] if available
	var modTime time.Time
	if len(parts) > 0 {
		modTime = getM3UModTime(parts[0])
	} else {
		modTime = time.Now()
	}

	switch len(parts) {
	case 1:
		return d.readDirHash(parts[0], modTime)
	case 2:
		return d.readDirOnDemand(ctx, parts[0], parts[1], modTime)
	case 3:
		return d.readDirOnDemandGroup(ctx, parts[0], parts[1], parts[2], modTime)
	case 4:
		return d.readDirOnDemandGroupSub(ctx, parts[0], parts[1], parts[2], parts[3], modTime)
	case 5:
		if parts[3] == dirSeries {
			return d.readDirSeries(ctx, parts[0], parts[1], parts[2], parts[4], modTime)
		}
	case 6:
		if parts[3] == dirSeries {
			return d.readDirSeason(ctx, parts[0], parts[1], parts[2], parts[4], parts[5], modTime)
		}
	}
	return nil, nil
}

func (d *webdavDir) readDirRoot() ([]os.FileInfo, error) {
	var infos []os.FileInfo
	hashes := slices.Sorted(maps.Keys(Settings.Files.M3U))
	for _, hash := range hashes {
		modTime := getM3UModTime(hash)
		infos = append(infos, &mkDirInfo{name: hash, modTime: modTime})
	}
	return infos, nil
}

func (d *webdavDir) readDirHash(hash string, modTime time.Time) ([]os.FileInfo, error) {
	var infos []os.FileInfo
	realPath := filepath.Join(System.Folder.Data, hash+".m3u")
	info, err := os.Stat(realPath)
	if err == nil {
		infos = append(infos, &mkFileInfo{name: fileListing, size: info.Size(), modTime: info.ModTime()})
	}
	infos = append(infos, &mkDirInfo{name: dirOnDemand, modTime: modTime})
	return infos, nil
}

func (d *webdavDir) readDirOnDemand(ctx context.Context, hash, sub string, modTime time.Time) ([]os.FileInfo, error) {
	if sub != dirOnDemand {
		return nil, nil
	}
	var infos []os.FileInfo
	groups := getGroupsForHash(ctx, hash)
	for _, g := range groups {
		infos = append(infos, &mkDirInfo{name: sanitizeGroupName(g), modTime: modTime})
	}
	return infos, nil
}

func (d *webdavDir) readDirOnDemandGroup(ctx context.Context, hash, sub, group string, modTime time.Time) ([]os.FileInfo, error) {
	if sub != dirOnDemand {
		return nil, nil
	}
	var infos []os.FileInfo

	// Check if we have individual streams
	if len(getIndividualStreamFiles(ctx, hash, group)) > 0 {
		infos = append(infos, &mkDirInfo{name: dirIndividual, modTime: modTime})
	}

	// Check if we have series
	if len(getSeriesList(ctx, hash, group)) > 0 {
		infos = append(infos, &mkDirInfo{name: dirSeries, modTime: modTime})
	}

	return infos, nil
}

func (d *webdavDir) readDirOnDemandGroupSub(ctx context.Context, hash, sub, group, subType string, modTime time.Time) ([]os.FileInfo, error) {
	if sub != dirOnDemand {
		return nil, nil
	}
	var infos []os.FileInfo

	if subType == dirIndividual {
		fileInfos := getIndividualStreamFiles(ctx, hash, group)

		// Ensure metadata for all files (videos and logos)
		ensureMetadataOptimized(ctx, hash, fileInfos)

		webdavCacheMutex.RLock()
		hc := webdavCache[hash]

		for _, f := range fileInfos {
			if hc == nil {
				continue
			}

			// Check video stream metadata (if video is unreachable, hide everything related)
			videoURL := f.Stream["url"]
			if _, ok := hc.FileMetadata[videoURL]; !ok {
				continue
			}

			// Check file metadata
			if f.TargetURL == "" {
				continue
			}

			meta, ok := hc.FileMetadata[f.TargetURL]
			if !ok {
				continue
			}

			size := meta.Size
			mt := modTime
			if !meta.ModTime.IsZero() {
				mt = meta.ModTime
			}

			infos = append(infos, &mkFileInfo{name: f.Name, size: size, modTime: mt})
		}
		webdavCacheMutex.RUnlock()
	} else if subType == dirSeries {
		series := getSeriesList(ctx, hash, group)
		for _, s := range series {
			infos = append(infos, &mkDirInfo{name: s, modTime: modTime})
		}
	}

	return infos, nil
}

func (d *webdavDir) readDirSeries(ctx context.Context, hash, sub, group, series string, modTime time.Time) ([]os.FileInfo, error) {
	if sub != dirOnDemand {
		return nil, nil
	}
	var infos []os.FileInfo
	seasons := getSeasonsList(ctx, hash, group, series)
	for _, s := range seasons {
		infos = append(infos, &mkDirInfo{name: s, modTime: modTime})
	}
	return infos, nil
}

func (d *webdavDir) readDirSeason(ctx context.Context, hash, sub, group, series, season string, modTime time.Time) ([]os.FileInfo, error) {
	if sub != dirOnDemand {
		return nil, nil
	}
	var infos []os.FileInfo
	fileInfos := getSeasonFiles(ctx, hash, group, series, season)

	// Ensure metadata for all files (videos and logos)
	ensureMetadataOptimized(ctx, hash, fileInfos)

	webdavCacheMutex.RLock()
	hc := webdavCache[hash]

	for _, f := range fileInfos {
		if hc == nil {
			continue
		}

		// Check video stream metadata (if video is unreachable, hide everything related)
		videoURL := f.Stream["url"]
		if _, ok := hc.FileMetadata[videoURL]; !ok {
			continue
		}

		// Check file metadata
		if f.TargetURL == "" {
			continue
		}

		meta, ok := hc.FileMetadata[f.TargetURL]
		if !ok {
			continue
		}

		size := meta.Size
		mt := modTime
		if !meta.ModTime.IsZero() {
			mt = meta.ModTime
		}

		infos = append(infos, &mkFileInfo{name: f.Name, size: size, modTime: mt})
	}
	webdavCacheMutex.RUnlock()
	return infos, nil
}

func (d *webdavDir) Stat() (_ os.FileInfo, err error) {
	// Use stored context if available, otherwise background
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_, span := otel.Tracer("webdav").Start(ctx, "Stat")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	span.SetAttributes(attribute.String("webdav.path", d.name))

	name := d.name
	if name == "" {
		// Root
		return &mkDirInfo{name: "", modTime: time.Now()}, nil
	}

	parts := strings.Split(name, "/")
	var modTime time.Time
	if len(parts) > 0 {
		modTime = getM3UModTime(parts[0])
	} else {
		modTime = time.Now()
	}

	return &mkDirInfo{name: path.Base(name), modTime: modTime}, nil
}

func (d *webdavDir) Write(p []byte) (n int, err error) {
	// Use stored context if available, otherwise background
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_, span := otel.Tracer("webdav").Start(ctx, "Write")
	defer span.End()
	span.SetAttributes(attribute.String("webdav.path", d.name))
	span.RecordError(os.ErrPermission)

	return 0, os.ErrPermission
}

// webdavStream implements webdav.File for streaming
type webdavStream struct {
	stream     map[string]string
	name       string
	ctx        context.Context
	readCloser io.ReadCloser
	pos        int64
	modTime    time.Time
	targetURL  string
	hash       string
	size       int64

	usingCache    bool
	cacheComplete bool
}

func (s *webdavStream) Close() (err error) {
	_, span := otel.Tracer("webdav").Start(s.ctx, "Close")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	span.SetAttributes(attribute.String("webdav.stream_name", s.name))

	if s.readCloser != nil {
		return s.readCloser.Close()
	}
	return nil
}

func (s *webdavStream) Read(p []byte) (n int, err error) {
	// Note: We avoid heavy tracing on Read per packet, but for completeness requested:
	// We'll trace it but perhaps this should be sampled or minimized in production.
	// For now we instrument it as requested.
	_, span := otel.Tracer("webdav").Start(s.ctx, "Read")
	defer span.End()
	span.SetAttributes(attribute.String("webdav.stream_name", s.name))

	if s.readCloser == nil {
		if err := s.openStream(s.pos); err != nil {
			span.RecordError(err)
			return 0, err
		}
	}
	n, err = s.readCloser.Read(p)
	if n > 0 {
		s.pos += int64(n)
	}

	if s.usingCache {
		// If we hit EOF on cache, and it's not complete, switch to upstream
		if err == io.EOF && !s.cacheComplete {
			s.readCloser.Close()
			s.readCloser = nil
			s.usingCache = false

			// Open upstream at current pos
			if openErr := s.openStream(s.pos); openErr != nil {
				span.RecordError(openErr)
				return n, openErr
			}

			// If we didn't read anything yet, read from upstream
			if n == 0 {
				var newN int
				var newErr error
				newN, newErr = s.readCloser.Read(p)
				if newN > 0 {
					s.pos += int64(newN)
				}
				return newN, newErr
			}

			// If we did read something, return it and let next Read call upstream
			return n, nil
		}
	}

	if err != nil && err != io.EOF {
		span.RecordError(err)
		// Attempt to retry
		const maxRetries = 3
		for i := 0; i < maxRetries; i++ {
			time.Sleep(200 * time.Millisecond) // Wait a bit before retrying

			// Close old connection
			if s.readCloser != nil {
				s.readCloser.Close()
				s.readCloser = nil
			}

			// Re-open stream at current position
			if openErr := s.openStream(s.pos); openErr != nil {
				continue // Retry open
			}

			// If we already read some bytes (n > 0), we can return those
			// and let the *next* Read call use the newly opened stream.
			if n > 0 {
				return n, nil
			}

			// If we haven't read anything yet (n == 0), try reading from new stream immediately
			var newN int
			var newErr error
			newN, newErr = s.readCloser.Read(p)
			if newN > 0 {
				s.pos += int64(newN)
				return newN, newErr
			}
			if newErr != nil && newErr != io.EOF {
				err = newErr // Update error for next retry loop
				continue     // Retry read
			}
			return newN, newErr
		}
	}

	return n, err
}

func (s *webdavStream) Seek(offset int64, whence int) (_ int64, err error) {
	_, span := otel.Tracer("webdav").Start(s.ctx, "Seek")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	span.SetAttributes(
		attribute.Int64("offset", offset),
		attribute.Int("whence", whence),
		attribute.Int64("file.size", s.size),
		attribute.String("target_url", s.targetURL),
	)

	// Ensure size is known if seeking from end
	if whence == io.SeekEnd && s.size == 0 {
		if info, err := resolveFileMetadata(s.ctx, s.hash, s.stream, s.targetURL, s.name, s.modTime); err == nil {
			s.size = info.Size()
			s.modTime = info.ModTime()
		}
	}

	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = s.pos + offset
	case io.SeekEnd:
		if s.size == 0 {
			// If size is unknown, we assume a very large size to allow streaming via ServeContent.
			// This allows GET requests to succeed even if Content-Length is missing upstream.
			// We use a safe large value (1TB) to ensure almost any content fits.
			const fakeSize = 1 << 40
			newPos = fakeSize + offset
		} else {
			newPos = s.size + offset
		}
	default:
		return 0, errors.New("invalid whence")
	}

	if newPos < 0 {
		return 0, errors.New("negative position")
	}

	// If position changed, we need to reopen the stream
	if newPos != s.pos {
		if s.readCloser != nil {
			s.readCloser.Close()
			s.readCloser = nil
		}
		s.pos = newPos
		// Actual open will happen on next Read
	}

	return newPos, nil
}

func (s *webdavStream) openStream(offset int64) (err error) {
	ctx, span := otel.Tracer("webdav").Start(s.ctx, "openStream")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()

	span.SetAttributes(
		attribute.Int64("offset", offset),
		attribute.String("webdav.stream_name", s.name),
	)

	url := s.targetURL
	if url == "" {
		// Fallback for safety, though targetURL should be set
		if u, ok := s.stream["url"]; ok {
			url = u
		}
	}
	if url == "" {
		err = errors.New("no url in stream")
		return err
	}

	// Cache Logic
	fc := getFileCache()
	path, meta, exists := fc.Get(url)

	// If request is within cache range (1MB) and we have it
	if exists && offset < filecache.MaxFileSize {
		f, err := os.Open(path)
		if err == nil {
			// Seek
			if _, err := f.Seek(offset, io.SeekStart); err == nil {
				s.readCloser = f
				s.usingCache = true
				s.cacheComplete = meta.Complete
				span.SetAttributes(attribute.Bool("webdav.cache_hit", true))
				return nil
			} else {
				f.Close()
			}
		}
	}

	if !exists {
		// Trigger cache download
		client := NewHTTPClient()
		fc.StartCaching(url, client, Settings.UserAgent)
	}

	span.SetAttributes(attribute.Bool("webdav.cache_hit", false))
	span.SetAttributes(attribute.String("http.url", url))

	// Use the context from the span to ensure propagation
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	// Handle Range header for seeking
	if s.pos > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", s.pos))
	}

	req.Header.Set("User-Agent", Settings.UserAgent)

	// Use a default client or one from System if available
	client := NewHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
		return fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	// Robustness: If we requested a range (s.pos > 0) but got 200 OK, the server
	// ignored the Range header (or dropped it on redirect). We must manually skip bytes.
	if s.pos > 0 && resp.StatusCode == http.StatusOK {
		span.AddEvent("webdav.range_ignored_by_upstream", trace.WithAttributes(
			attribute.Int64("bytes_to_skip", s.pos),
		))
		// Discard the prefix we didn't want
		_, err := io.CopyN(io.Discard, resp.Body, s.pos)
		if err != nil {
			resp.Body.Close()
			span.RecordError(err)
			return fmt.Errorf("failed to skip %d bytes: %w", s.pos, err)
		}
	}

	s.readCloser = resp.Body
	return nil
}

func (s *webdavStream) Readdir(count int) ([]os.FileInfo, error) {
	_, span := otel.Tracer("webdav").Start(s.ctx, "Readdir")
	defer span.End()
	span.SetAttributes(attribute.String("webdav.stream_name", s.name))
	span.RecordError(os.ErrPermission)
	return nil, os.ErrPermission
}

func (s *webdavStream) Stat() (_ os.FileInfo, err error) {
	_, span := otel.Tracer("webdav").Start(s.ctx, "Stat")
	defer span.End()
	defer func() {
		if err != nil {
			span.RecordError(err)
		}
	}()
	span.SetAttributes(attribute.String("webdav.stream_name", s.name))

	if s.size > 0 {
		return &mkFileInfo{name: s.name, size: s.size, modTime: s.modTime}, nil
	}

	// Try to resolve metadata
	if info, err := resolveFileMetadata(s.ctx, s.hash, s.stream, s.targetURL, s.name, s.modTime); err == nil {
		s.size = info.Size()
		s.modTime = info.ModTime()
		return info, nil
	}

	return &mkFileInfo{name: s.name, size: 0, modTime: s.modTime}, nil
}

func (s *webdavStream) Write(p []byte) (n int, err error) {
	_, span := otel.Tracer("webdav").Start(s.ctx, "Write")
	defer span.End()
	span.SetAttributes(attribute.String("webdav.stream_name", s.name))
	span.RecordError(os.ErrPermission)
	return 0, os.ErrPermission
}

// Helpers

var (
	webdavCache      = make(map[string]*HashCache)
	webdavCacheMutex sync.RWMutex
)

type HashCache struct {
	Groups          []string
	Series          map[string][]string              // Key: Group
	Seasons         map[seasonKey][]string           // Key: Group, Series
	SeasonFiles     map[seasonFileKey][]FileStreamInfo // Key: Group, Series, Season
	IndividualFiles map[string][]FileStreamInfo      // Key: Group
	FileMetadata    map[string]FileMeta              // Key: Stream URL
}

type FileMeta struct {
	Size    int64
	ModTime time.Time
}

type FileStreamInfo struct {
	Name      string
	Stream    map[string]string
	TargetURL string
}

type seasonKey struct {
	Group  string
	Series string
}

type seasonFileKey struct {
	Group  string
	Series string
	Season string
}

// ClearWebDAVCache clears the WebDAV cache for a specific hash or all if empty
func ClearWebDAVCache(hash string) {
	webdavCacheMutex.Lock()
	defer webdavCacheMutex.Unlock()
	if hash == "" {
		webdavCache = make(map[string]*HashCache)
	} else {
		delete(webdavCache, hash)
	}
}

func getStreamMetadata(stream map[string]string) (FileMeta, bool) {
	var meta FileMeta
	found := false

	// Check M3U attributes for size
	// Potential keys: "size", "bytes"
	if val, ok := stream["size"]; ok {
		if s, err := strconv.ParseInt(val, 10, 64); err == nil {
			meta.Size = s
			found = true
		}
	} else if val, ok := stream["bytes"]; ok {
		if s, err := strconv.ParseInt(val, 10, 64); err == nil {
			meta.Size = s
			found = true
		}
	}

	// Check M3U attributes for mod time
	// Potential keys: "time", "date", "mtime"
	// Formats can vary. We'll try RFC3339, then generic fallback if needed.
	timeKeys := []string{"time", "date", "mtime", "modification-time"}
	for _, k := range timeKeys {
		if val, ok := stream[k]; ok {
			// Try parsing with common formats
			formats := []string{
				time.RFC3339,
				time.RFC1123,
				time.RFC1123Z,
				"2006-01-02 15:04:05",
				"2006-01-02",
			}
			for _, f := range formats {
				if t, err := time.Parse(f, val); err == nil {
					meta.ModTime = t
					found = true
					break
				}
			}
			// Unix timestamp check
			if i, err := strconv.ParseInt(val, 10, 64); err == nil {
				// Assuming seconds if small, ms if large?
				// Simple heuristic: if > 30000000000 (year 2920), likely ms?
				// Actually Unix time is around 1.7e9.
				meta.ModTime = time.Unix(i, 0)
				found = true
				break
			}
		}
		if !meta.ModTime.IsZero() {
			break
		}
	}

	return meta, found
}

var fetchRemoteMetadataFunc = defaultFetchRemoteMetadata

func defaultFetchRemoteMetadata(ctx context.Context, urlStr string) (FileMeta, error) {
	ctx, span := otel.Tracer("webdav").Start(ctx, "fetchRemoteMetadata")
	defer span.End()
	span.SetAttributes(attribute.String("http.url", urlStr))

	var meta FileMeta
	client := NewHTTPClient()
	client.Timeout = 5 * time.Second

	req, err := http.NewRequestWithContext(ctx, "HEAD", urlStr, nil)
	if err != nil {
		span.RecordError(err)
		return meta, err
	}

	req.Header.Set("User-Agent", Settings.UserAgent)

	resp, err := client.Do(req)

	// Check if HEAD succeeded
	if err == nil && resp.StatusCode == http.StatusOK && resp.ContentLength > 0 {
		defer resp.Body.Close()
		span.SetAttributes(attribute.Int64("http.response.content_length", resp.ContentLength))

		meta.Size = resp.ContentLength
		if lastMod := resp.Header.Get("Last-Modified"); lastMod != "" {
			if t, err := http.ParseTime(lastMod); err == nil {
				meta.ModTime = t
			}
		}
		return meta, nil
	}

	// Even if HEAD didn't give us content length, we might have got ModTime
	if err == nil && resp.StatusCode == http.StatusOK {
		if lastMod := resp.Header.Get("Last-Modified"); lastMod != "" {
			if t, err := http.ParseTime(lastMod); err == nil {
				meta.ModTime = t
			}
		}
	}

	// HEAD failed or returned no content length, clean up and try fallback
	if err == nil {
		resp.Body.Close()
	}

	if err != nil {
		span.RecordError(err)
	} else if resp.StatusCode != http.StatusOK {
		span.RecordError(fmt.Errorf("status %d", resp.StatusCode))
	}

	// Fallback: Use cache logic and GET first MB
	// 1. Trigger cache
	fc := getFileCache()
	fc.StartCaching(urlStr, NewHTTPClient(), Settings.UserAgent)

	// 2. GET request with Range
	req, err = http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return meta, err
	}

	req.Header.Set("User-Agent", Settings.UserAgent)
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", filecache.MaxFileSize-1))

	resp, err = client.Do(req)
	if err != nil {
		return meta, err
	}
	defer resp.Body.Close()

	// 3. Check response
	if resp.StatusCode == http.StatusOK {
		meta.Size = resp.ContentLength
	} else if resp.StatusCode == http.StatusPartialContent {
		// Parse Content-Range: bytes start-end/total
		cr := resp.Header.Get("Content-Range")
		parts := strings.Split(cr, "/")
		if len(parts) == 2 {
			totalStr := strings.TrimSpace(parts[1])
			if total, err := strconv.ParseInt(totalStr, 10, 64); err == nil {
				meta.Size = total
			}
		}
	}

	// If size is still 0 (unknown or failed parsing), try requesting the last few bytes.
	// Some servers might not return total size for the first range but might for a suffix range.
	if meta.Size <= 0 {
		req, err = http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if err == nil {
			req.Header.Set("User-Agent", Settings.UserAgent)
			req.Header.Set("Range", "bytes=-1024") // Request last 1KB

			resp, err = client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusPartialContent {
					cr := resp.Header.Get("Content-Range")
					parts := strings.Split(cr, "/")
					if len(parts) == 2 {
						// Clean up parts[1] (remove spaces, etc)
						totalStr := strings.TrimSpace(parts[1])
						if total, err := strconv.ParseInt(totalStr, 10, 64); err == nil {
							meta.Size = total
						}
					}
				}
			}
		}
	}

	// If size is still 0 (unknown or failed parsing), return error
	if meta.Size <= 0 {
		return meta, errors.New("failed to determine file size")
	}

	// Try to get ModTime from GET response
	if lastMod := resp.Header.Get("Last-Modified"); lastMod != "" {
		if t, err := http.ParseTime(lastMod); err == nil {
			meta.ModTime = t
		}
	}

	span.SetAttributes(
		attribute.Int64("http.response.content_length", meta.Size),
		attribute.String("metadata.fallback", "true"),
	)
	return meta, nil
}

// Improved ensureMetadata that checks M3U first
func ensureMetadataOptimized(ctx context.Context, hash string, files []FileStreamInfo) {
	ctx, span := otel.Tracer("webdav").Start(ctx, "ensureMetadataOptimized")
	defer span.End()

	// Identify streams needing metadata
	var toFetch []string

	webdavCacheMutex.Lock()
	hc, ok := webdavCache[hash]
	if !ok {
		hc = &HashCache{
			Series:          make(map[string][]string),
			Seasons:         make(map[seasonKey][]string),
			SeasonFiles:     make(map[seasonFileKey][]FileStreamInfo),
			IndividualFiles: make(map[string][]FileStreamInfo),
			FileMetadata:    make(map[string]FileMeta),
		}
		webdavCache[hash] = hc
	}
	if hc.FileMetadata == nil {
		hc.FileMetadata = make(map[string]FileMeta)
	}

	// Check cache and M3U attributes
	for _, file := range files {
		urlStr := file.TargetURL
		if urlStr == "" {
			continue
		}

		// If already in cache, skip
		if _, exists := hc.FileMetadata[urlStr]; exists {
			continue
		}

		// Check M3U attributes (only if this is the video stream)
		isVideo := urlStr == file.Stream["url"]
		if isVideo {
			if meta, found := getStreamMetadata(file.Stream); found {
				hc.FileMetadata[urlStr] = meta
				continue
			}
		}

		// Needs remote fetch (fallback for video, or always for logo)
		toFetch = append(toFetch, urlStr)
	}
	webdavCacheMutex.Unlock()

	if len(toFetch) == 0 {
		return
	}

	// Fetch in parallel
	var wg sync.WaitGroup
	sem := make(chan struct{}, 20)
	results := make(map[string]FileMeta)
	var resultsMutex sync.Mutex

	for _, urlStr := range toFetch {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			meta, err := fetchRemoteMetadataFunc(ctx, u)
			if err == nil {
				resultsMutex.Lock()
				results[u] = meta
				resultsMutex.Unlock()
			}
		}(urlStr)
	}
	wg.Wait()

	if len(results) > 0 {
		webdavCacheMutex.Lock()
		for u, meta := range results {
			hc.FileMetadata[u] = meta
		}
		webdavCacheMutex.Unlock()
	}
}

func getM3UModTime(hash string) time.Time {
	jsonPath := filepath.Join(System.Folder.Data, hash+".json")

	// Try to read mod_time from JSON content
	if data, err := os.ReadFile(jsonPath); err == nil {
		var meta struct {
			ModTime time.Time `json:"mod_time"`
		}
		if err := json.Unmarshal(data, &meta); err == nil && !meta.ModTime.IsZero() {
			return meta.ModTime
		}
	}

	// Fallback to file mod time if JSON content parsing failed or file exists but empty/invalid
	if info, err := os.Stat(jsonPath); err == nil {
		return info.ModTime()
	}

	realPath := filepath.Join(System.Folder.Data, hash+".m3u")
	info, err := os.Stat(realPath)
	if err != nil {
		return time.Now()
	}
	return info.ModTime()
}

var sanitizeRegex = regexp.MustCompile(`[^a-zA-Z0-9.\-_ ():]`)
var seriesRegex = regexp.MustCompile(`(?i)^(.*?)[_.\s]*S(\d{1,3})[_.\s]*E\d{1,3}`)

func parseSeries(name string) (string, string, int, bool) {
	matches := seriesRegex.FindStringSubmatch(name)
	if len(matches) < 3 {
		return "", "", 0, false
	}
	rawSeriesName := matches[1]
	originalRaw := rawSeriesName
	seasonStr := matches[2]

	// Trim trailing separators that might have been captured
	rawSeriesName = strings.TrimSuffix(rawSeriesName, " -")
	rawSeriesName = strings.TrimSuffix(rawSeriesName, "_-_")

	// Handle standard " - " separator
	lastHyphen := strings.LastIndex(rawSeriesName, " - ")
	if lastHyphen != -1 {
		rawSeriesName = rawSeriesName[lastHyphen+3:]
	} else {
		// Handle "_-_" separator (User scenario: text_-_Foo_Bar_S01_E01)
		lastUnderscoreHyphen := strings.LastIndex(rawSeriesName, "_-_")
		if lastUnderscoreHyphen != -1 {
			rawSeriesName = rawSeriesName[lastUnderscoreHyphen+3:]
			// If we found _-_, we assume the rest of the name uses underscores as spaces
			rawSeriesName = strings.ReplaceAll(rawSeriesName, "_", " ")
		}
	}

	sNum, _ := strconv.Atoi(seasonStr)
	return strings.TrimSpace(rawSeriesName), originalRaw, sNum, true
}

func sanitizeFilename(name string) string {
	return sanitizeRegex.ReplaceAllString(name, "_")
}

func sanitizeGroupName(name string) string {
	// Replace forward slashes to avoid path conflicts
	return strings.ReplaceAll(name, "/", "_")
}

func getExtensionFromURL(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return path.Ext(urlStr)
	}
	return path.Ext(u.Path)
}

func getStreamsForGroup(ctx context.Context, hash, group string) []map[string]string {
	_, span := otel.Tracer("webdav").Start(ctx, "getStreamsForGroup")
	defer span.End()

	var results []map[string]string

	for _, s := range Data.Streams.All {
		stream, ok := s.(map[string]string)
		if !ok {
			continue
		}

		if stream["_file.m3u.id"] == hash {
			if isVOD(stream) {
				g := stream["group-title"]
				if g == "" {
					g = "Uncategorized"
				}

				if sanitizeGroupName(g) == group {
					results = append(results, stream)
				}
			}
		}
	}
	span.SetAttributes(attribute.Int("streams.count", len(results)))
	return results
}

func getIndividualStreams(ctx context.Context, hash, group string) []map[string]string {
	_, span := otel.Tracer("webdav").Start(ctx, "getIndividualStreams")
	defer span.End()

	all := getStreamsForGroup(ctx, hash, group)
	var res []map[string]string
	for _, s := range all {
		if _, _, _, isSeries := parseSeries(s["name"]); !isSeries {
			res = append(res, s)
		}
	}
	span.SetAttributes(attribute.Int("streams.count", len(res)))
	return res
}

func getSeriesStreams(ctx context.Context, hash, group, seriesName string, season int) []map[string]string {
	_, span := otel.Tracer("webdav").Start(ctx, "getSeriesStreams")
	defer span.End()

	all := getStreamsForGroup(ctx, hash, group)
	var res []map[string]string
	for _, s := range all {
		name, _, sNum, isSeries := parseSeries(s["name"])
		if isSeries && sanitizeGroupName(name) == seriesName && sNum == season {
			res = append(res, s)
		}
	}
	span.SetAttributes(attribute.Int("streams.count", len(res)))
	return res
}

func getGroupsForHash(ctx context.Context, hash string) []string {
	_, span := otel.Tracer("webdav").Start(ctx, "getGroupsForHash")
	defer span.End()

	webdavCacheMutex.RLock()
	if hc, ok := webdavCache[hash]; ok {
		if hc.Groups != nil {
			webdavCacheMutex.RUnlock()
			return hc.Groups
		}
	}
	webdavCacheMutex.RUnlock()

	groupsMap := make(map[string]bool)

	for _, s := range Data.Streams.All {
		stream, ok := s.(map[string]string)
		if !ok {
			continue
		}

		if stream["_file.m3u.id"] == hash {
			if isVOD(stream) {
				g := stream["group-title"]
				if g == "" {
					g = "Uncategorized"
				}
				groupsMap[g] = true
			}
		}
	}

	groups := slices.Sorted(maps.Keys(groupsMap))

	webdavCacheMutex.Lock()
	hc, ok := webdavCache[hash]
	if !ok {
		hc = &HashCache{
			Series:          make(map[string][]string),
			Seasons:         make(map[seasonKey][]string),
			SeasonFiles:     make(map[seasonFileKey][]FileStreamInfo),
			IndividualFiles: make(map[string][]FileStreamInfo),
			FileMetadata:    make(map[string]FileMeta),
		}
		webdavCache[hash] = hc
	}
	hc.Groups = groups
	webdavCacheMutex.Unlock()

	span.SetAttributes(attribute.Int("groups.count", len(groups)))
	return groups
}

func getIndividualStreamFiles(ctx context.Context, hash, group string) []FileStreamInfo {
	_, span := otel.Tracer("webdav").Start(ctx, "getIndividualStreamFiles")
	defer span.End()

	webdavCacheMutex.RLock()
	if hc, ok := webdavCache[hash]; ok {
		if list, ok := hc.IndividualFiles[group]; ok {
			webdavCacheMutex.RUnlock()
			return list
		}
	}
	webdavCacheMutex.RUnlock()

	streams := getIndividualStreams(ctx, hash, group)
	files := generateFileStreamInfos(ctx, streams)

	webdavCacheMutex.Lock()
	hc, ok := webdavCache[hash]
	if !ok {
		hc = &HashCache{
			Series:          make(map[string][]string),
			Seasons:         make(map[seasonKey][]string),
			SeasonFiles:     make(map[seasonFileKey][]FileStreamInfo),
			IndividualFiles: make(map[string][]FileStreamInfo),
			FileMetadata:    make(map[string]FileMeta),
		}
		webdavCache[hash] = hc
	}
	if hc.IndividualFiles == nil {
		hc.IndividualFiles = make(map[string][]FileStreamInfo)
	}
	hc.IndividualFiles[group] = files
	webdavCacheMutex.Unlock()

	return files
}

func getSeasonFiles(ctx context.Context, hash, group, series, seasonStr string) []FileStreamInfo {
	_, span := otel.Tracer("webdav").Start(ctx, "getSeasonFiles")
	defer span.End()

	key := seasonFileKey{Group: group, Series: series, Season: seasonStr}
	webdavCacheMutex.RLock()
	if hc, ok := webdavCache[hash]; ok {
		if list, ok := hc.SeasonFiles[key]; ok {
			webdavCacheMutex.RUnlock()
			return list
		}
	}
	webdavCacheMutex.RUnlock()

	// seasonStr is "Season X"
	// Parse X
	parts := strings.Split(seasonStr, " ")
	if len(parts) < 2 {
		return nil
	}
	sNum, _ := strconv.Atoi(parts[1])

	streams := getSeriesStreams(ctx, hash, group, series, sNum)
	files := generateFileStreamInfos(ctx, streams)

	webdavCacheMutex.Lock()
	hc, ok := webdavCache[hash]
	if !ok {
		hc = &HashCache{
			Series:          make(map[string][]string),
			Seasons:         make(map[seasonKey][]string),
			SeasonFiles:     make(map[seasonFileKey][]FileStreamInfo),
			IndividualFiles: make(map[string][]FileStreamInfo),
			FileMetadata:    make(map[string]FileMeta),
		}
		webdavCache[hash] = hc
	}
	if hc.SeasonFiles == nil {
		hc.SeasonFiles = make(map[seasonFileKey][]FileStreamInfo)
	}
	hc.SeasonFiles[key] = files
	webdavCacheMutex.Unlock()

	return files
}

func generateFileStreamInfos(ctx context.Context, streams []map[string]string) []FileStreamInfo {
	_, span := otel.Tracer("webdav").Start(ctx, "generateFileStreamInfos")
	defer span.End()

	var files []FileStreamInfo
	// We need to track counts for base names to ensure videos and logos match
	// Key: baseName (without extension) -> count
	baseNameCounts := make(map[string]int)

	// First pass: determine unique base names
	type streamBase struct {
		baseName string
		count    int
	}
	streamBases := make([]streamBase, len(streams))

	for i, stream := range streams {
		name := stream["name"]

		// Series cleaning logic
		if cleanName, rawPrefix, _, isSeries := parseSeries(name); isSeries {
			// Avoid redundant regex execution
			if rawPrefix != "" {
				remainder := name[len(rawPrefix):]
				// Normalize separator to " - " for Plex compatibility
				trimmedRemainder := strings.TrimLeft(remainder, " _-")
				name = cleanName + " - " + trimmedRemainder
			}
		}

		baseName := sanitizeFilename(name)
		count := baseNameCounts[baseName]
		streamBases[i] = streamBase{baseName: baseName, count: count}
		baseNameCounts[baseName]++
	}

	for i, stream := range streams {
		sb := streamBases[i]

		// Video File
		videoExt := getExtensionFromURL(stream["url"])
		if videoExt == "" {
			videoExt = ".mp4"
		}

		var videoName string
		if sb.count > 0 {
			videoName = fmt.Sprintf("%s_%d%s", sb.baseName, sb.count, videoExt)
		} else {
			videoName = sb.baseName + videoExt
		}

		files = append(files, FileStreamInfo{
			Name:      videoName,
			Stream:    stream,
			TargetURL: stream["url"],
		})

		// Logo File
		if logo := stream["tvg-logo"]; logo != "" {
			logoExt := getExtensionFromURL(logo)
			if logoExt == "" {
				logoExt = ".jpg" // Default
			}

			var logoName string
			if sb.count > 0 {
				logoName = fmt.Sprintf("%s_%d%s", sb.baseName, sb.count, logoExt)
			} else {
				logoName = sb.baseName + logoExt
			}

			files = append(files, FileStreamInfo{
				Name:      logoName,
				Stream:    stream,
				TargetURL: logo,
			})
		}
	}
	return files
}

func getSeriesList(ctx context.Context, hash, group string) []string {
	_, span := otel.Tracer("webdav").Start(ctx, "getSeriesList")
	defer span.End()

	webdavCacheMutex.RLock()
	if hc, ok := webdavCache[hash]; ok {
		if list, ok := hc.Series[group]; ok {
			webdavCacheMutex.RUnlock()
			return list
		}
	}
	webdavCacheMutex.RUnlock()

	all := getStreamsForGroup(ctx, hash, group)
	seen := make(map[string]bool)
	for _, s := range all {
		name, _, _, isSeries := parseSeries(s["name"])
		if isSeries {
			seen[sanitizeGroupName(name)] = true
		}
	}
	var res []string
	for k := range seen {
		res = append(res, k)
	}
	slices.Sort(res)

	webdavCacheMutex.Lock()
	hc, ok := webdavCache[hash]
	if !ok {
		hc = &HashCache{
			Series:          make(map[string][]string),
			Seasons:         make(map[seasonKey][]string),
			SeasonFiles:     make(map[seasonFileKey][]FileStreamInfo),
			IndividualFiles: make(map[string][]FileStreamInfo),
			FileMetadata:    make(map[string]FileMeta),
		}
		webdavCache[hash] = hc
	}
	if hc.Series == nil {
		hc.Series = make(map[string][]string)
	}
	hc.Series[group] = res
	webdavCacheMutex.Unlock()

	return res
}

func getSeasonsList(ctx context.Context, hash, group, series string) []string {
	_, span := otel.Tracer("webdav").Start(ctx, "getSeasonsList")
	defer span.End()

	key := seasonKey{Group: group, Series: series}
	webdavCacheMutex.RLock()
	if hc, ok := webdavCache[hash]; ok {
		if list, ok := hc.Seasons[key]; ok {
			webdavCacheMutex.RUnlock()
			return list
		}
	}
	webdavCacheMutex.RUnlock()

	// series is already sanitizedGroupName
	all := getStreamsForGroup(ctx, hash, group)
	seen := make(map[int]bool)
	for _, s := range all {
		name, _, sNum, isSeries := parseSeries(s["name"])
		if isSeries && sanitizeGroupName(name) == series {
			seen[sNum] = true
		}
	}

	var nums []int
	for k := range seen {
		nums = append(nums, k)
	}
	slices.Sort(nums)

	var res []string
	for _, n := range nums {
		res = append(res, fmt.Sprintf("Season %d", n))
	}

	webdavCacheMutex.Lock()
	hc, ok := webdavCache[hash]
	if !ok {
		hc = &HashCache{
			Series:          make(map[string][]string),
			Seasons:         make(map[seasonKey][]string),
			SeasonFiles:     make(map[seasonFileKey][]FileStreamInfo),
			IndividualFiles: make(map[string][]FileStreamInfo),
			FileMetadata:    make(map[string]FileMeta),
		}
		webdavCache[hash] = hc
	}
	if hc.Seasons == nil {
		hc.Seasons = make(map[seasonKey][]string)
	}
	hc.Seasons[key] = res
	webdavCacheMutex.Unlock()

	return res
}

func findIndividualStream(ctx context.Context, hash, group, filename string) (map[string]string, string, error) {
	_, span := otel.Tracer("webdav").Start(ctx, "findIndividualStream")
	defer span.End()

	// Use getIndividualStreamFiles which is cached, instead of getIndividualStreams + generateFileStreamInfos
	// This was an optimization in upstream (origin/main) that we should preserve,
	// but we must pass the context for tracing.
	files := getIndividualStreamFiles(ctx, hash, group)
	for _, f := range files {
		if f.Name == filename {
			return f.Stream, f.TargetURL, nil
		}
	}
	return nil, "", os.ErrNotExist
}

func findSeriesStream(ctx context.Context, hash, group, series, seasonStr, filename string) (map[string]string, string, error) {
	_, span := otel.Tracer("webdav").Start(ctx, "findSeriesStream")
	defer span.End()

	// Use getSeasonFiles which is cached, instead of getSeriesStreams + generateFileStreamInfos
	// This was an optimization in upstream (origin/main)
	files := getSeasonFiles(ctx, hash, group, series, seasonStr)
	for _, f := range files {
		if f.Name == filename {
			return f.Stream, f.TargetURL, nil
		}
	}
	return nil, "", os.ErrNotExist
}

func isVOD(stream map[string]string) bool {
	// 1. Check extension first (priority over duration)
	urlStr := stream["url"]
	ext := strings.ToLower(getExtensionFromURL(urlStr))

	// List of extensions typically associated with VOD
	vodExts := []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mpg", ".mpeg", ".m4v"}
	if slices.Contains(vodExts, ext) {
		return true // Is VOD
	}

	// List of extensions typically associated with streams
	streamExts := []string{".m3u8", ".ts", ".php", ".pl"} // .php/pl often used for live stream redirects
	if slices.Contains(streamExts, ext) {
		return false // Is Live
	}

	// 2. Fallback to duration check if extension is ambiguous or unknown
	if val, ok := stream["_duration"]; ok {
		duration, err := strconv.Atoi(val)
		if err == nil {
			if duration > 0 {
				return true // VOD
			}
			// If duration <= 0, we assume Live (since extension check failed to find VOD)
			return false
		}
	}

	// Default to false (Live) if unsure, to be safe and comply with "only show if it ISN'T a stream"
	return false
}
