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
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/webdav"
)

// WebDAVFS implements the webdav.FileSystem interface
type WebDAVFS struct {
}

// mkDirInfo implements os.FileInfo for a directory
type mkDirInfo struct {
	name string
}

func (d *mkDirInfo) Name() string       { return d.name }
func (d *mkDirInfo) Size() int64        { return 0 }
func (d *mkDirInfo) Mode() os.FileMode  { return os.ModeDir | 0555 }
func (d *mkDirInfo) ModTime() time.Time { return time.Now() }
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
		return fs.openOnDemandStream(ctx, hash, parts[1], parts[2], parts[3])
	default:
		return nil, os.ErrNotExist
	}
}

func (fs *WebDAVFS) openHashDir(hash string) (webdav.File, error) {
	return &webdavDir{name: hash}, nil
}

func (fs *WebDAVFS) openHashSubDir(hash, sub string) (webdav.File, error) {
	if sub == "listing.m3u" {
		realPath := filepath.Join(System.Folder.Data, hash+".m3u")
		f, err := os.Open(realPath)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	if sub == "On Demand" {
		return &webdavDir{name: path.Join(hash, sub)}, nil
	}
	return nil, os.ErrNotExist
}

func (fs *WebDAVFS) openOnDemandGroupDir(hash, sub, group string) (webdav.File, error) {
	if sub != "On Demand" {
		return nil, os.ErrNotExist
	}
	groups := getGroupsForHash(hash)
	found := false
	for _, g := range groups {
		if sanitizeGroupName(g) == group {
			found = true
			break
		}
	}
	if !found {
		return nil, os.ErrNotExist
	}
	return &webdavDir{name: path.Join(hash, sub, group)}, nil
}

func (fs *WebDAVFS) openOnDemandStream(ctx context.Context, hash, sub, group, filename string) (webdav.File, error) {
	if sub != "On Demand" {
		return nil, os.ErrNotExist
	}
	stream, err := findStreamByFilename(hash, group, filename)
	if err != nil {
		return nil, os.ErrNotExist
	}
	return &webdavStream{
		stream: stream,
		name:   filename,
		ctx:    ctx,
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

	switch len(parts) {
	case 1:
		return fs.removeAllHashDir(hash)
	case 2:
		return fs.removeAllHashSubDir(hash, parts[1])
	case 3:
		return fs.removeAllOnDemandGroupDir(hash, parts[1], parts[2])
	case 4:
		return fs.removeAllOnDemandStream(hash, parts[1], parts[2], parts[3])
	default:
		return os.ErrNotExist
	}
}

func (fs *WebDAVFS) removeAllHashDir(hash string) error {
	return os.ErrPermission
}

func (fs *WebDAVFS) removeAllHashSubDir(hash, sub string) error {
	if sub == "listing.m3u" {
		return os.ErrPermission
	}
	if sub == "On Demand" {
		return os.ErrPermission
	}
	return os.ErrNotExist
}

func (fs *WebDAVFS) removeAllOnDemandGroupDir(hash, sub, group string) error {
	if sub != "On Demand" {
		return os.ErrNotExist
	}
	groups := getGroupsForHash(hash)
	found := false
	for _, g := range groups {
		if sanitizeGroupName(g) == group {
			found = true
			break
		}
	}
	if !found {
		return os.ErrNotExist
	}
	return os.ErrPermission
}

func (fs *WebDAVFS) removeAllOnDemandStream(hash, sub, group, filename string) error {
	if sub != "On Demand" {
		return os.ErrNotExist
	}
	_, err := findStreamByFilename(hash, group, filename)
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
		return &mkDirInfo{name: ""}, nil
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
		return fs.statHashDir(hash)
	case 2:
		return fs.statHashSubDir(hash, parts[1])
	case 3:
		return fs.statOnDemandGroupDir(hash, parts[1], parts[2])
	case 4:
		return fs.statOnDemandStream(hash, parts[1], parts[2], parts[3])
	default:
		return nil, os.ErrNotExist
	}
}

func (fs *WebDAVFS) statHashDir(hash string) (os.FileInfo, error) {
	return &mkDirInfo{name: hash}, nil
}

func (fs *WebDAVFS) statHashSubDir(hash, sub string) (os.FileInfo, error) {
	if sub == "listing.m3u" {
		realPath := filepath.Join(System.Folder.Data, hash+".m3u")
		info, err := os.Stat(realPath)
		if err != nil {
			return nil, err
		}
		return &mkFileInfo{name: "listing.m3u", size: info.Size(), modTime: info.ModTime()}, nil
	}
	if sub == "On Demand" {
		return &mkDirInfo{name: "On Demand"}, nil
	}
	return nil, os.ErrNotExist
}

func (fs *WebDAVFS) statOnDemandGroupDir(hash, sub, group string) (os.FileInfo, error) {
	if sub != "On Demand" {
		return nil, os.ErrNotExist
	}
	groups := getGroupsForHash(hash)
	found := false
	for _, g := range groups {
		if sanitizeGroupName(g) == group {
			found = true
			break
		}
	}
	if !found {
		return nil, os.ErrNotExist
	}
	return &mkDirInfo{name: group}, nil
}

func (fs *WebDAVFS) statOnDemandStream(hash, sub, group, filename string) (os.FileInfo, error) {
	if sub != "On Demand" {
		return nil, os.ErrNotExist
	}
	_, err := findStreamByFilename(hash, group, filename)
	if err != nil {
		return nil, os.ErrNotExist
	}
	// Size 0 as we stream it
	return &mkFileInfo{name: filename, size: 0, modTime: time.Now()}, nil
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
	switch len(parts) {
	case 1:
		return d.readDirHash(parts[0])
	case 2:
		return d.readDirOnDemand(parts[0], parts[1])
	case 3:
		return d.readDirOnDemandGroup(parts[0], parts[1], parts[2])
	default:
		return nil, nil
	}
}

func (d *webdavDir) readDirRoot() ([]os.FileInfo, error) {
	var infos []os.FileInfo
	var hashes []string
	for hash := range Settings.Files.M3U {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	for _, hash := range hashes {
		infos = append(infos, &mkDirInfo{name: hash})
	}
	return infos, nil
}

func (d *webdavDir) readDirHash(hash string) ([]os.FileInfo, error) {
	var infos []os.FileInfo
	realPath := filepath.Join(System.Folder.Data, hash+".m3u")
	info, err := os.Stat(realPath)
	if err == nil {
		infos = append(infos, &mkFileInfo{name: "listing.m3u", size: info.Size(), modTime: info.ModTime()})
	}
	infos = append(infos, &mkDirInfo{name: "On Demand"})
	return infos, nil
}

func (d *webdavDir) readDirOnDemand(hash, sub string) ([]os.FileInfo, error) {
	if sub != "On Demand" {
		return nil, nil
	}
	var infos []os.FileInfo
	groups := getGroupsForHash(hash)
	for _, g := range groups {
		infos = append(infos, &mkDirInfo{name: sanitizeGroupName(g)})
	}
	return infos, nil
}

func (d *webdavDir) readDirOnDemandGroup(hash, sub, group string) ([]os.FileInfo, error) {
	if sub != "On Demand" {
		return nil, nil
	}
	var infos []os.FileInfo
	files := getStreamFilesForGroup(hash, group)
	for _, f := range files {
		infos = append(infos, &mkFileInfo{name: f, size: 0, modTime: time.Now()})
	}
	return infos, nil
}

func (d *webdavDir) Stat() (os.FileInfo, error) {
	name := d.name
	if name == "" {
		return &mkDirInfo{name: ""}, nil
	}
	return &mkDirInfo{name: path.Base(name)}, nil
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
		// Actual open will happen on next Read, but we can try to open now or defer.
		// Deferring is safer to avoid errors during Seek, but returning nil error implies success.
		// Let's defer opening to Read.
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
	return &mkFileInfo{name: s.name, size: 0, modTime: time.Now()}, nil
}

func (s *webdavStream) Write(p []byte) (n int, err error) {
	return 0, os.ErrPermission
}

// Helpers

var sanitizeRegex = regexp.MustCompile(`[^a-zA-Z0-9.\-_]`)

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

func findStreamByFilename(hash, group, filename string) (map[string]string, error) {
	// Reconstruct the logic used in getStreamFilesForGroup to match filename
	// This is inefficient but avoids storing state.
	// Filter streams for hash and group
	streams := getStreamsForGroup(hash, group)

	// Since we might have duplicates in names, we need to match exactly how we generated filenames.
	// We'll generate the map of filename -> stream

	nameCount := make(map[string]int)

	for _, stream := range streams {
		name := stream["name"]
		ext := getExtensionFromURL(stream["url"])
		if ext == "" {
			ext = ".mp4" // Default extension
		}

		baseName := sanitizeFilename(name)
		finalName := baseName + ext

		// Handle duplicates
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

func getStreamsForGroup(hash, group string) []map[string]string {
	var results []map[string]string

	// Data.Streams.All is []any
	for _, s := range Data.Streams.All {
		stream, ok := s.(map[string]string)
		if !ok {
			continue
		}

		if stream["_file.m3u.id"] == hash {
			// Check if it's VOD or Stream
			if isVOD(stream) {
				// Handle empty group
				g := stream["group-title"]
				if g == "" {
					g = "Uncategorized"
				}

				// Compare sanitized group name because the client requests with sanitized name
				if sanitizeGroupName(g) == group {
					results = append(results, stream)
				}
			}
		}
	}
	return results
}

func getGroupsForHash(hash string) []string {
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
	sort.Strings(groups)
	return groups
}

func getStreamFilesForGroup(hash, group string) []string {
	streams := getStreamsForGroup(hash, group)
	var files []string
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

		files = append(files, finalName)
	}
	return files
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
