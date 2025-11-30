package src

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
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

	// Root directory
	if name == "" {
		return &webdavDir{name: ""}, nil
	}

	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		// This should be a hash directory
		hash := parts[0]
		if _, ok := Settings.Files.M3U[hash]; ok {
			return &webdavDir{name: hash}, nil
		}
		return nil, os.ErrNotExist
	} else if len(parts) == 2 {
		// This should be listing.m3u inside a hash directory
		hash := parts[0]
		filename := parts[1]
		if filename != "listing.m3u" {
			return nil, os.ErrNotExist
		}

		if _, ok := Settings.Files.M3U[hash]; !ok {
			return nil, os.ErrNotExist
		}

		realPath := filepath.Join(System.Folder.Data, hash+".m3u")
		f, err := os.Open(realPath)
		if err != nil {
			return nil, err
		}
		return f, nil
	}

	return nil, os.ErrNotExist
}

// RemoveAll returns an error as the filesystem is read-only
func (fs *WebDAVFS) RemoveAll(ctx context.Context, name string) error {
	return os.ErrPermission
}

// Rename returns an error as the filesystem is read-only
func (fs *WebDAVFS) Rename(ctx context.Context, oldName, newName string) error {
	return os.ErrPermission
}

// Stat returns file info
func (fs *WebDAVFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	name = strings.TrimPrefix(name, "/")

	// Root directory
	if name == "" {
		return &mkDirInfo{name: ""}, nil
	}

	parts := strings.Split(name, "/")
	if len(parts) == 1 {
		// This should be a hash directory
		hash := parts[0]
		if _, ok := Settings.Files.M3U[hash]; ok {
			return &mkDirInfo{name: hash}, nil
		}
		return nil, os.ErrNotExist
	} else if len(parts) == 2 {
		// This should be listing.m3u inside a hash directory
		hash := parts[0]
		filename := parts[1]
		if filename != "listing.m3u" {
			return nil, os.ErrNotExist
		}

		if _, ok := Settings.Files.M3U[hash]; !ok {
			return nil, os.ErrNotExist
		}

		realPath := filepath.Join(System.Folder.Data, hash+".m3u")
		info, err := os.Stat(realPath)
		if err != nil {
			return nil, err
		}
		return &mkFileInfo{name: "listing.m3u", size: info.Size(), modTime: info.ModTime()}, nil
	}

	return nil, os.ErrNotExist
}

// webdavDir represents a virtual directory
type webdavDir struct {
	name string
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
	var infos []os.FileInfo

	if d.name == "" {
		// Root directory: list all M3U hashes
		var hashes []string
		for hash := range Settings.Files.M3U {
			hashes = append(hashes, hash)
		}
		sort.Strings(hashes)

		for _, hash := range hashes {
			infos = append(infos, &mkDirInfo{name: hash})
		}
	} else {
		// Hash directory: list listing.m3u
		realPath := filepath.Join(System.Folder.Data, d.name+".m3u")
		info, err := os.Stat(realPath)
		if err == nil {
			infos = append(infos, &mkFileInfo{name: "listing.m3u", size: info.Size(), modTime: info.ModTime()})
		}
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

func (d *webdavDir) Stat() (os.FileInfo, error) {
	return &mkDirInfo{name: d.name}, nil
}

func (d *webdavDir) Write(p []byte) (n int, err error) {
	return 0, os.ErrPermission
}
