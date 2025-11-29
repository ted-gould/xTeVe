package src

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/webdav"
)

// WebDAVHandler returns a http.Handler for the WebDAV server
func WebDAVHandler() http.Handler {
	return &webdav.Handler{
		Prefix:     "/dav",
		FileSystem: &webdavFS{},
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil {
				// Suppress harmless errors or log them for debugging
				// fmt.Printf("WebDAV Error: %s %s: %v\n", r.Method, r.URL.Path, err)
			}
		},
	}
}

// webdavFS implements webdav.FileSystem to serve M3U files.
type webdavFS struct{}

func (fs *webdavFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return os.ErrPermission
}

func (fs *webdavFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, "/")

	if name == "" {
		// Root directory
		return &webdavDir{
			name:     "/",
			modTime:  time.Now(),
			children: getRootChildren(),
		}, nil
	}

	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		// Subdirectory (hash of m3u file)
		hash := parts[0]
		if _, ok := Settings.Files.M3U[hash]; ok {
			return &webdavDir{
				name:    hash,
				modTime: time.Now(),
				children: []os.FileInfo{
					&webdavFileInfo{name: "listing.m3u", size: getM3UFileSize(hash), modTime: time.Now(), isDir: false},
				},
			}, nil
		}
		return nil, os.ErrNotExist
	}

	if len(parts) == 2 {
		// File inside subdirectory
		hash := parts[0]
		filename := parts[1]
		if filename == "listing.m3u" {
			if _, ok := Settings.Files.M3U[hash]; ok {
				filePath := filepath.Join(System.Folder.Data, hash+".m3u")
				content, err := readByteFromFile(filePath)
				if err != nil {
					return nil, err
				}

				return &webdavFile{
					name:    "listing.m3u",
					content: content,
					modTime: time.Now(),
					pos:     0,
				}, nil
			}
		}
		return nil, os.ErrNotExist
	}

	return nil, os.ErrNotExist
}

func (fs *webdavFS) RemoveAll(ctx context.Context, name string) error {
	return os.ErrPermission
}

func (fs *webdavFS) Rename(ctx context.Context, oldName, newName string) error {
	return os.ErrPermission
}

func (fs *webdavFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimSuffix(name, "/")

	if name == "" {
		return &webdavFileInfo{name: "/", size: 0, modTime: time.Now(), isDir: true}, nil
	}

	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		hash := parts[0]
		if _, ok := Settings.Files.M3U[hash]; ok {
			return &webdavFileInfo{name: hash, size: 0, modTime: time.Now(), isDir: true}, nil
		}
		return nil, os.ErrNotExist
	}

	if len(parts) == 2 {
		hash := parts[0]
		filename := parts[1]
		if filename == "listing.m3u" {
			if _, ok := Settings.Files.M3U[hash]; ok {
				return &webdavFileInfo{name: "listing.m3u", size: getM3UFileSize(hash), modTime: time.Now(), isDir: false}, nil
			}
		}
		return nil, os.ErrNotExist
	}

	return nil, os.ErrNotExist
}

// Helper functions

func getRootChildren() []os.FileInfo {
	var children []os.FileInfo
	for hash := range Settings.Files.M3U {
		children = append(children, &webdavFileInfo{
			name:    hash,
			size:    0,
			modTime: time.Now(),
			isDir:   true,
		})
	}
	return children
}

func getM3UFileSize(hash string) int64 {
	filePath := filepath.Join(System.Folder.Data, hash+".m3u")
	info, err := os.Stat(filePath)
	if err == nil {
		return info.Size()
	}
	return 0
}

// webdavDir implements webdav.File for directories
type webdavDir struct {
	name     string
	modTime  time.Time
	children []os.FileInfo
	pos      int
}

func (d *webdavDir) Close() error { return nil }
func (d *webdavDir) Read(p []byte) (n int, err error) {
	return 0, os.ErrInvalid // Directories cannot be read
}
func (d *webdavDir) Seek(offset int64, whence int) (int64, error) {
	return 0, os.ErrInvalid
}
func (d *webdavDir) Readdir(count int) ([]os.FileInfo, error) {
	if d.pos >= len(d.children) {
		if count > 0 {
			return nil, io.EOF
		}
		return nil, nil
	}
	if count <= 0 {
		infos := d.children[d.pos:]
		d.pos = len(d.children)
		return infos, nil
	}
	end := d.pos + count
	if end > len(d.children) {
		end = len(d.children)
	}
	infos := d.children[d.pos:end]
	d.pos = end
	return infos, nil
}
func (d *webdavDir) Stat() (os.FileInfo, error) {
	return &webdavFileInfo{name: d.name, size: 0, modTime: d.modTime, isDir: true}, nil
}
func (d *webdavDir) Write(p []byte) (n int, err error) { return 0, os.ErrPermission }

// webdavFile implements webdav.File for files
type webdavFile struct {
	name    string
	content []byte
	modTime time.Time
	pos     int64
}

func (f *webdavFile) Close() error { return nil }
func (f *webdavFile) Read(p []byte) (n int, err error) {
	if f.pos >= int64(len(f.content)) {
		return 0, io.EOF
	}
	n = copy(p, f.content[f.pos:])
	f.pos += int64(n)
	return n, nil
}
func (f *webdavFile) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case 0:
		newPos = offset
	case 1:
		newPos = f.pos + offset
	case 2:
		newPos = int64(len(f.content)) + offset
	}
	if newPos < 0 {
		return 0, os.ErrInvalid
	}
	f.pos = newPos
	return newPos, nil
}
func (f *webdavFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, os.ErrInvalid
}
func (f *webdavFile) Stat() (os.FileInfo, error) {
	return &webdavFileInfo{name: f.name, size: int64(len(f.content)), modTime: f.modTime, isDir: false}, nil
}
func (f *webdavFile) Write(p []byte) (n int, err error) { return 0, os.ErrPermission }

// webdavFileInfo implements os.FileInfo
type webdavFileInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}

func (fi *webdavFileInfo) Name() string       { return fi.name }
func (fi *webdavFileInfo) Size() int64        { return fi.size }
func (fi *webdavFileInfo) Mode() os.FileMode  {
	if fi.isDir {
		return os.ModeDir | 0555
	}
	return 0444
}
func (fi *webdavFileInfo) ModTime() time.Time { return fi.modTime }
func (fi *webdavFileInfo) IsDir() bool        { return fi.isDir }
func (fi *webdavFileInfo) Sys() any           { return nil }
