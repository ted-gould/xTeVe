package src

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWebDAVFS(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "xteve_webdav_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Save original values
	origFolderData := System.Folder.Data
	origFilesM3U := Settings.Files.M3U
	defer func() {
		System.Folder.Data = origFolderData
		Settings.Files.M3U = origFilesM3U
	}()

	System.Folder.Data = tempDir
	Settings.Files.M3U = make(map[string]interface{})

	// Create a dummy M3U file
	hash := "testhash"
	content := "#EXTM3U\n#EXTINF:-1,Test\nhttp://test.com/stream"
	err = os.WriteFile(filepath.Join(tempDir, hash+".m3u"), []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Test Playlist"}

	fs := &WebDAVFS{}
	ctx := context.Background()

	// Test root listing
	f, err := fs.OpenFile(ctx, "/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open root: %v", err)
	}
	infos, err := f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read root dir: %v", err)
	}
	found := false
	for _, info := range infos {
		if info.Name() == hash && info.IsDir() {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Root listing did not contain hash directory %s", hash)
	}
	f.Close()

	// Test hash directory listing
	f, err = fs.OpenFile(ctx, "/"+hash, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open hash dir: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read hash dir: %v", err)
	}
	found = false
	for _, info := range infos {
		if info.Name() == "listing.m3u" && !info.IsDir() {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Hash dir listing did not contain listing.m3u")
	}
	f.Close()

	// Test file content
	f, err = fs.OpenFile(ctx, "/"+hash+"/listing.m3u", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open listing.m3u: %v", err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("Failed to read listing.m3u: %v", err)
	}
	if string(data) != content {
		t.Errorf("Content mismatch. Got %s, want %s", string(data), content)
	}
	f.Close()

	// Test non-existent hash
	_, err = fs.OpenFile(ctx, "/invalidhash", os.O_RDONLY, 0)
	if !os.IsNotExist(err) {
		t.Errorf("Expected NotExist error for invalid hash, got %v", err)
	}

	// Test write permission (should fail)
	err = fs.Mkdir(ctx, "/newdir", 0755)
	if err == nil {
		t.Error("Mkdir should fail")
	}
}
