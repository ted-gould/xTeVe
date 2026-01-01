package src

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"
)

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
	return os.ErrPermission
}

// OpenFile opens a file or directory
func (fs *WebDAVFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, "/")

	// Root directory
	if name == "" {
		return &webdavDir{name: ""}, nil
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
		return fs.openHashDir(hash)
	case 2:
		return fs.openHashSubDir(hash, parts[1])
	case 3:
		return fs.openOnDemandGroupDir(hash, parts[1], parts[2])
	case 4:
		return fs.openOnDemandGroupSubDir(hash, parts[1], parts[2], parts[3])
	case 5:
		if parts[3] == dirSeries {
			return fs.openOnDemandSeriesDir(hash, parts[1], parts[2], parts[4])
		}
		if parts[3] == dirIndividual {
			return fs.openOnDemandIndividualStream(ctx, hash, parts[1], parts[2], parts[4])
		}
	case 6:
		if parts[3] == dirSeries {
			return fs.openOnDemandSeasonDir(hash, parts[1], parts[2], parts[4], parts[5])
		}
	case 7:
		if parts[3] == dirSeries {
			return fs.openOnDemandSeriesStream(ctx, hash, parts[1], parts[2], parts[4], parts[5], parts[6])
		}
	}

	return nil, os.ErrNotExist
}

func (fs *WebDAVFS) openHashDir(hash string) (webdav.File, error) {
	return &webdavDir{name: hash}, nil
}

func (fs *WebDAVFS) openHashSubDir(hash, sub string) (webdav.File, error) {
	if sub == fileListing {
		realPath := filepath.Join(System.Folder.Data, hash+".m3u")
		f, err := os.Open(realPath)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	if sub == dirOnDemand {
		return &webdavDir{name: path.Join(hash, sub)}, nil
	}
	return nil, os.ErrNotExist
}

func (fs *WebDAVFS) groupExists(hash, group string) bool {
	groups := getGroupsForHash(hash)
	for _, g := range groups {
		if sanitizeGroupName(g) == group {
			return true
		}
	}
	return false
}

func (fs *WebDAVFS) openOnDemandGroupDir(hash, sub, group string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	if !fs.groupExists(hash, group) {
		return nil, os.ErrNotExist
	}
	return &webdavDir{name: path.Join(hash, sub, group)}, nil
}

func (fs *WebDAVFS) openOnDemandGroupSubDir(hash, sub, group, typeDir string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	if !fs.groupExists(hash, group) {
		return nil, os.ErrNotExist
	}
	if typeDir == dirSeries || typeDir == dirIndividual {
		return &webdavDir{name: path.Join(hash, sub, group, typeDir)}, nil
	}
	return nil, os.ErrNotExist
}

func (fs *WebDAVFS) openOnDemandSeriesDir(hash, sub, group, series string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	if !fs.groupExists(hash, group) {
		return nil, os.ErrNotExist
	}
	// Check if series exists
	seriesList := getSeriesList(hash, group)
	found := false
	for _, s := range seriesList {
		if s == series {
			found = true
			break
		}
	}
	if !found {
		return nil, os.ErrNotExist
	}
	return &webdavDir{name: path.Join(hash, sub, group, dirSeries, series)}, nil
}

func (fs *WebDAVFS) openOnDemandSeasonDir(hash, sub, group, series, season string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	if !fs.groupExists(hash, group) {
		return nil, os.ErrNotExist
	}
	// Check if season exists
	seasons := getSeasonsList(hash, group, series)
	found := false
	for _, s := range seasons {
		if s == season {
			found = true
			break
		}
	}
	if !found {
		return nil, os.ErrNotExist
	}
	return &webdavDir{name: path.Join(hash, sub, group, dirSeries, series, season)}, nil
}

func (fs *WebDAVFS) openOnDemandIndividualStream(ctx context.Context, hash, sub, group, filename string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	stream, err := findIndividualStream(hash, group, filename)
	if err != nil {
		return nil, os.ErrNotExist
	}
	modTime := getM3UModTime(hash)
	return &webdavStream{
		stream:  stream,
		name:    filename,
		ctx:     ctx,
		modTime: modTime,
	}, nil
}

func (fs *WebDAVFS) openOnDemandSeriesStream(ctx context.Context, hash, sub, group, series, season, filename string) (webdav.File, error) {
	if sub != dirOnDemand {
		return nil, os.ErrNotExist
	}
	stream, err := findSeriesStream(hash, group, series, season, filename)
	if err != nil {
		return nil, os.ErrNotExist
	}
	modTime := getM3UModTime(hash)
	return &webdavStream{
		stream:  stream,
		name:    filename,
		ctx:     ctx,
		modTime: modTime,
	}, nil
}

// RemoveAll returns an error as the filesystem is read-only
func (fs *WebDAVFS) RemoveAll(ctx context.Context, name string) error {
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, "/")

	if name == "" {
		return os.ErrPermission
	}

	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return os.ErrNotExist
	}

	hash := parts[0]
	if _, ok := Settings.Files.M3U[hash]; !ok {
		return os.ErrNotExist
	}

	// Just check if it exists and return ErrPermission, otherwise ErrNotExist
	_, err := fs.Stat(ctx, name)
	if err != nil {
		return os.ErrNotExist
	}
	return os.ErrPermission
}

// Rename returns an error as the filesystem is read-only
func (fs *WebDAVFS) Rename(ctx context.Context, oldName, newName string) error {
	return os.ErrPermission
}

// Stat returns file info
func (fs *WebDAVFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
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
		if parts[1] == dirOnDemand && fs.groupExists(hash, parts[2]) {
			return &mkDirInfo{name: parts[2], modTime: modTime}, nil
		}
	case 4:
		// Series or Individual dir
		if parts[1] == dirOnDemand && fs.groupExists(hash, parts[2]) {
			if parts[3] == dirSeries || parts[3] == dirIndividual {
				return &mkDirInfo{name: parts[3], modTime: modTime}, nil
			}
		}
	case 5:
		if parts[1] == dirOnDemand && fs.groupExists(hash, parts[2]) {
			if parts[3] == dirIndividual {
				// File in Individual
				_, err := findIndividualStream(hash, parts[2], parts[4])
				if err == nil {
					return &mkFileInfo{name: parts[4], size: 0, modTime: modTime}, nil
				}
			} else if parts[3] == dirSeries {
				// Series Dir
				seriesList := getSeriesList(hash, parts[2])
				for _, s := range seriesList {
					if s == parts[4] {
						return &mkDirInfo{name: parts[4], modTime: modTime}, nil
					}
				}
			}
		}
	case 6:
		if parts[1] == dirOnDemand && fs.groupExists(hash, parts[2]) && parts[3] == dirSeries {
			// Season Dir
			seasons := getSeasonsList(hash, parts[2], parts[4])
			for _, s := range seasons {
				if s == parts[5] {
					return &mkDirInfo{name: parts[5], modTime: modTime}, nil
				}
			}
		}
	case 7:
		if parts[1] == dirOnDemand && fs.groupExists(hash, parts[2]) && parts[3] == dirSeries {
			// File in Series
			_, err := findSeriesStream(hash, parts[2], parts[4], parts[5], parts[6])
			if err == nil {
				return &mkFileInfo{name: parts[6], size: 0, modTime: modTime}, nil
			}
		}
	}

	return nil, os.ErrNotExist
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
}

func (d *webdavDir) Close() error {
	return nil
}

func (d *webdavDir) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (d *webdavDir) Seek(offset int64, whence int) (int64, error) {
	return 0, nil
}

func (d *webdavDir) Readdir(count int) ([]os.FileInfo, error) {
	infos, err := d.collectInfos()
	if err != nil {
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
		return res, nil
	}

	return infos, nil
}

func (d *webdavDir) collectInfos() ([]os.FileInfo, error) {
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
		return d.readDirOnDemand(parts[0], parts[1], modTime)
	case 3:
		return d.readDirOnDemandGroup(parts[0], parts[1], parts[2], modTime)
	case 4:
		return d.readDirOnDemandGroupSub(parts[0], parts[1], parts[2], parts[3], modTime)
	case 5:
		if parts[3] == dirSeries {
			return d.readDirSeries(parts[0], parts[1], parts[2], parts[4], modTime)
		}
	case 6:
		if parts[3] == dirSeries {
			return d.readDirSeason(parts[0], parts[1], parts[2], parts[4], parts[5], modTime)
		}
	}
	return nil, nil
}

func (d *webdavDir) readDirRoot() ([]os.FileInfo, error) {
	var infos []os.FileInfo
	var hashes []string
	for hash := range Settings.Files.M3U {
		hashes = append(hashes, hash)
	}
	slices.Sort(hashes)
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

func (d *webdavDir) readDirOnDemand(hash, sub string, modTime time.Time) ([]os.FileInfo, error) {
	if sub != dirOnDemand {
		return nil, nil
	}
	var infos []os.FileInfo
	groups := getGroupsForHash(hash)
	for _, g := range groups {
		infos = append(infos, &mkDirInfo{name: sanitizeGroupName(g), modTime: modTime})
	}
	return infos, nil
}

func (d *webdavDir) readDirOnDemandGroup(hash, sub, group string, modTime time.Time) ([]os.FileInfo, error) {
	if sub != dirOnDemand {
		return nil, nil
	}
	var infos []os.FileInfo

	// Check if we have individual streams
	if len(getIndividualStreamFiles(hash, group)) > 0 {
		infos = append(infos, &mkDirInfo{name: dirIndividual, modTime: modTime})
	}

	// Check if we have series
	if len(getSeriesList(hash, group)) > 0 {
		infos = append(infos, &mkDirInfo{name: dirSeries, modTime: modTime})
	}

	return infos, nil
}

func (d *webdavDir) readDirOnDemandGroupSub(hash, sub, group, subType string, modTime time.Time) ([]os.FileInfo, error) {
	if sub != dirOnDemand {
		return nil, nil
	}
	var infos []os.FileInfo

	if subType == dirIndividual {
		files := getIndividualStreamFiles(hash, group)
		for _, f := range files {
			infos = append(infos, &mkFileInfo{name: f, size: 0, modTime: modTime})
		}
	} else if subType == dirSeries {
		series := getSeriesList(hash, group)
		for _, s := range series {
			infos = append(infos, &mkDirInfo{name: s, modTime: modTime})
		}
	}

	return infos, nil
}

func (d *webdavDir) readDirSeries(hash, sub, group, series string, modTime time.Time) ([]os.FileInfo, error) {
	if sub != dirOnDemand {
		return nil, nil
	}
	var infos []os.FileInfo
	seasons := getSeasonsList(hash, group, series)
	for _, s := range seasons {
		infos = append(infos, &mkDirInfo{name: s, modTime: modTime})
	}
	return infos, nil
}

func (d *webdavDir) readDirSeason(hash, sub, group, series, season string, modTime time.Time) ([]os.FileInfo, error) {
	if sub != dirOnDemand {
		return nil, nil
	}
	var infos []os.FileInfo
	files := getSeasonFiles(hash, group, series, season)
	for _, f := range files {
		infos = append(infos, &mkFileInfo{name: f, size: 0, modTime: modTime})
	}
	return infos, nil
}

func (d *webdavDir) Stat() (os.FileInfo, error) {
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
}

func (s *webdavStream) Close() error {
	if s.readCloser != nil {
		return s.readCloser.Close()
	}
	return nil
}

func (s *webdavStream) Read(p []byte) (n int, err error) {
	if s.readCloser == nil {
		if err := s.openStream(0); err != nil {
			return 0, err
		}
	}
	n, err = s.readCloser.Read(p)
	if n > 0 {
		s.pos += int64(n)
	}

	if err != nil && err != io.EOF {
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

func (s *webdavStream) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = s.pos + offset
	case io.SeekEnd:
		return 0, errors.New("seeking from end not supported")
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

func (s *webdavStream) openStream(offset int64) error {
	url, ok := s.stream["url"]
	if !ok || url == "" {
		return errors.New("no url in stream")
	}

	req, err := http.NewRequestWithContext(s.ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	// Handle Range header for seeking
	if s.pos > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", s.pos))
	}

	// Use a default client or one from System if available
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	s.readCloser = resp.Body
	return nil
}

func (s *webdavStream) Readdir(count int) ([]os.FileInfo, error) {
	return nil, os.ErrPermission
}

func (s *webdavStream) Stat() (os.FileInfo, error) {
	return &mkFileInfo{name: s.name, size: 0, modTime: s.modTime}, nil
}

func (s *webdavStream) Write(p []byte) (n int, err error) {
	return 0, os.ErrPermission
}

// Helpers

var (
	webdavCache      = make(map[string]*HashCache)
	webdavCacheMutex sync.RWMutex
)

type HashCache struct {
	Groups          []string
	Series          map[string][]string        // Key: Group
	Seasons         map[seasonKey][]string     // Key: Group, Series
	SeasonFiles     map[seasonFileKey][]string // Key: Group, Series, Season
	IndividualFiles map[string][]string        // Key: Group
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

func getM3UModTime(hash string) time.Time {
	realPath := filepath.Join(System.Folder.Data, hash+".m3u")
	info, err := os.Stat(realPath)
	if err != nil {
		return time.Now()
	}
	return info.ModTime()
}

var sanitizeRegex = regexp.MustCompile(`[^a-zA-Z0-9.\-_ ]`)
var seriesRegex = regexp.MustCompile(`(?i)^(.*?)[_\s]*S(\d{1,3})[_\s]*E\d{1,3}`)

func parseSeries(name string) (string, int, bool) {
	matches := seriesRegex.FindStringSubmatch(name)
	if len(matches) < 3 {
		return "", 0, false
	}
	rawSeriesName := matches[1]
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
	return strings.TrimSpace(rawSeriesName), sNum, true
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

func getStreamsForGroup(hash, group string) []map[string]string {
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
	return results
}

func getIndividualStreams(hash, group string) []map[string]string {
	all := getStreamsForGroup(hash, group)
	var res []map[string]string
	for _, s := range all {
		if _, _, isSeries := parseSeries(s["name"]); !isSeries {
			res = append(res, s)
		}
	}
	return res
}

func getSeriesStreams(hash, group, seriesName string, season int) []map[string]string {
	all := getStreamsForGroup(hash, group)
	var res []map[string]string
	for _, s := range all {
		name, sNum, isSeries := parseSeries(s["name"])
		if isSeries && sanitizeGroupName(name) == seriesName && sNum == season {
			res = append(res, s)
		}
	}
	return res
}

func getGroupsForHash(hash string) []string {
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

	var groups []string
	for g := range groupsMap {
		groups = append(groups, g)
	}
	slices.Sort(groups)

	webdavCacheMutex.Lock()
	hc, ok := webdavCache[hash]
	if !ok {
		hc = &HashCache{
			Series:          make(map[string][]string),
			Seasons:         make(map[seasonKey][]string),
			SeasonFiles:     make(map[seasonFileKey][]string),
			IndividualFiles: make(map[string][]string),
		}
		webdavCache[hash] = hc
	}
	hc.Groups = groups
	webdavCacheMutex.Unlock()

	return groups
}

func getIndividualStreamFiles(hash, group string) []string {
	webdavCacheMutex.RLock()
	if hc, ok := webdavCache[hash]; ok {
		if list, ok := hc.IndividualFiles[group]; ok {
			webdavCacheMutex.RUnlock()
			return list
		}
	}
	webdavCacheMutex.RUnlock()

	streams := getIndividualStreams(hash, group)
	files := generateFilenames(streams)

	webdavCacheMutex.Lock()
	hc, ok := webdavCache[hash]
	if !ok {
		hc = &HashCache{
			Series:          make(map[string][]string),
			Seasons:         make(map[seasonKey][]string),
			SeasonFiles:     make(map[seasonFileKey][]string),
			IndividualFiles: make(map[string][]string),
		}
		webdavCache[hash] = hc
	}
	if hc.IndividualFiles == nil {
		hc.IndividualFiles = make(map[string][]string)
	}
	hc.IndividualFiles[group] = files
	webdavCacheMutex.Unlock()

	return files
}

func getSeasonFiles(hash, group, series, seasonStr string) []string {
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

	streams := getSeriesStreams(hash, group, series, sNum)
	files := generateFilenames(streams)

	webdavCacheMutex.Lock()
	hc, ok := webdavCache[hash]
	if !ok {
		hc = &HashCache{
			Series:          make(map[string][]string),
			Seasons:         make(map[seasonKey][]string),
			SeasonFiles:     make(map[seasonFileKey][]string),
			IndividualFiles: make(map[string][]string),
		}
		webdavCache[hash] = hc
	}
	if hc.SeasonFiles == nil {
		hc.SeasonFiles = make(map[seasonFileKey][]string)
	}
	hc.SeasonFiles[key] = files
	webdavCacheMutex.Unlock()

	return files
}

func generateFilenames(streams []map[string]string) []string {
	var files []string
	nameCount := make(map[string]int)

	for _, stream := range streams {
		name := stream["name"]

		// If it's a series, attempt to use the clean series name (stripping prefix)
		if cleanName, _, isSeries := parseSeries(name); isSeries {
			matches := seriesRegex.FindStringSubmatch(name)
			if len(matches) >= 2 {
				rawSeriesName := matches[1]
				// Replace the raw series name part with the clean name
				// We assume matches[1] is at the start of the string because the regex starts with ^
				remainder := name[len(rawSeriesName):]
				name = cleanName + remainder
			}
		}

		ext := getExtensionFromURL(stream["url"])
		if ext == "" {
			ext = ".mp4"
		}

		baseName := sanitizeFilename(name)
		finalName := baseName + ext

		count := nameCount[finalName]
		if count > 0 {
			finalName = fmt.Sprintf("%s_%d%s", baseName, count, ext)
		}
		nameCount[baseName+ext]++

		files = append(files, finalName)
	}
	return files
}

func getSeriesList(hash, group string) []string {
	webdavCacheMutex.RLock()
	if hc, ok := webdavCache[hash]; ok {
		if list, ok := hc.Series[group]; ok {
			webdavCacheMutex.RUnlock()
			return list
		}
	}
	webdavCacheMutex.RUnlock()

	all := getStreamsForGroup(hash, group)
	seen := make(map[string]bool)
	for _, s := range all {
		name, _, isSeries := parseSeries(s["name"])
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
			SeasonFiles:     make(map[seasonFileKey][]string),
			IndividualFiles: make(map[string][]string),
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

func getSeasonsList(hash, group, series string) []string {
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
	all := getStreamsForGroup(hash, group)
	seen := make(map[int]bool)
	for _, s := range all {
		name, sNum, isSeries := parseSeries(s["name"])
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
			SeasonFiles:     make(map[seasonFileKey][]string),
			IndividualFiles: make(map[string][]string),
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

func findIndividualStream(hash, group, filename string) (map[string]string, error) {
	streams := getIndividualStreams(hash, group)
	return findStreamInList(streams, filename)
}

func findSeriesStream(hash, group, series, seasonStr, filename string) (map[string]string, error) {
    parts := strings.Split(seasonStr, " ")
    if len(parts) < 2 {
        return nil, os.ErrNotExist
    }
    sNum, _ := strconv.Atoi(parts[1])

	streams := getSeriesStreams(hash, group, series, sNum)
	return findStreamInList(streams, filename)
}

func findStreamInList(streams []map[string]string, filename string) (map[string]string, error) {
	nameCount := make(map[string]int)

	for _, stream := range streams {
		name := stream["name"]
		ext := getExtensionFromURL(stream["url"])
		if ext == "" {
			ext = ".mp4"
		}

		baseName := sanitizeFilename(name)
		finalName := baseName + ext

		count := nameCount[finalName]
		if count > 0 {
			finalName = fmt.Sprintf("%s_%d%s", baseName, count, ext)
		}
		nameCount[baseName+ext]++

		if finalName == filename {
			return stream, nil
		}
	}

	return nil, os.ErrNotExist
}

func isVOD(stream map[string]string) bool {
	// 1. Check extension first (priority over duration)
	urlStr := stream["url"]
	ext := strings.ToLower(getExtensionFromURL(urlStr))

	// List of extensions typically associated with VOD
	vodExts := []string{".mp4", ".mkv", ".avi", ".mov", ".wmv", ".flv", ".webm", ".mpg", ".mpeg", ".m4v"}
	for _, e := range vodExts {
		if ext == e {
			return true // Is VOD
		}
	}

	// List of extensions typically associated with streams
	streamExts := []string{".m3u8", ".ts", ".php", ".pl"} // .php/pl often used for live stream redirects
	for _, e := range streamExts {
		if ext == e {
			return false // Is Live
		}
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
