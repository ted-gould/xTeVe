package src

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWebDAVFS_Filtering(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "xteve_webdav_filtering_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Save original values
	origFolderData := System.Folder.Data
	origFilesM3U := Settings.Files.M3U
	origStreamsAll := Data.Streams.All
	origFetchFunc := fetchRemoteMetadataFunc

	defer func() {
		System.Folder.Data = origFolderData
		Settings.Files.M3U = origFilesM3U
		Data.Streams.All = origStreamsAll
		fetchRemoteMetadataFunc = origFetchFunc
	}()

	System.Folder.Data = tempDir
	Settings.Files.M3U = make(map[string]interface{})

	hash := "filterhash"
	ClearWebDAVCache(hash)
	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Filter Test Playlist"}

	// Create dummy M3U
	err = os.WriteFile(filepath.Join(tempDir, hash+".m3u"), []byte("#EXTM3U"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Define URLs
	urlGood := "http://example.com/good.mp4"
	urlBad := "http://example.com/bad.mp4"
	urlLogoGood := "http://example.com/logo_good.jpg"
	urlLogoBad := "http://example.com/logo_bad.jpg"

	// Mock fetchRemoteMetadataFunc
	fetchRemoteMetadataFunc = func(ctx context.Context, urlStr string) (FileMeta, error) {
		if urlStr == urlGood || urlStr == urlLogoGood {
			return FileMeta{Size: 1024, ModTime: time.Now()}, nil
		}
		if urlStr == urlBad || urlStr == urlLogoBad {
			return FileMeta{}, errors.New("fetch failed")
		}
		return FileMeta{}, errors.New("unknown url")
	}

	Data.Streams.All = []interface{}{
		// Case 1: Good Video -> Should be listed
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Group 1",
			"name":         "Good Video",
			"url":          urlGood,
			"_duration":    "3600",
		},
		// Case 2: Bad Video -> Should NOT be listed
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Group 1",
			"name":         "Bad Video",
			"url":          urlBad,
			"_duration":    "3600",
		},
		// Case 3: Good Video, Good Logo -> Both listed
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Group 1",
			"name":         "Good with Good Logo",
			"url":          urlGood, // Reusing good url
			"tvg-logo":     urlLogoGood,
			"_duration":    "3600",
		},
		// Case 4: Bad Video, Good Logo -> Neither listed
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Group 1",
			"name":         "Bad with Good Logo",
			"url":          urlBad,
			"tvg-logo":     urlLogoGood,
			"_duration":    "3600",
		},
		// Case 5: Good Video, Bad Logo -> Video listed, Logo NOT listed
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Group 1",
			"name":         "Good with Bad Logo",
			"url":          urlGood,
			"tvg-logo":     urlLogoBad,
			"_duration":    "3600",
		},
	}

	fs := &WebDAVFS{}
	ctx := context.Background()

	// List "Group 1" in "On Demand" -> "Individual"
	// Path: /dav/<hash>/On Demand/Group 1/Individual
	path := "/" + hash + "/" + dirOnDemand + "/Group 1/" + dirIndividual
	f, err := fs.OpenFile(ctx, path, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open dir: %v", err)
	}
	infos, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		t.Fatalf("Failed to read dir: %v", err)
	}

	// Analyze results
	files := make(map[string]bool)
	for _, info := range infos {
		files[info.Name()] = true
	}

	// Expectations
	// 1. Good Video.mp4 -> Present
	if !files["Good Video.mp4"] {
		t.Errorf("Expected 'Good Video.mp4' to be listed")
	}

	// 2. Bad Video.mp4 -> Present (Fallback behavior)
	if !files["Bad Video.mp4"] {
		t.Errorf("Expected 'Bad Video.mp4' to be listed (fallback)")
	}

	// 3. Good with Good Logo.mp4 -> Present
	if !files["Good with Good Logo.mp4"] {
		t.Errorf("Expected 'Good with Good Logo.mp4' to be listed")
	}
	// 3. Good with Good Logo.jpg -> Present
	if !files["Good with Good Logo.jpg"] {
		t.Errorf("Expected 'Good with Good Logo.jpg' to be listed")
	}

	// 4. Bad with Good Logo.mp4 -> Present (Fallback behavior)
	if !files["Bad with Good Logo.mp4"] {
		t.Errorf("Expected 'Bad with Good Logo.mp4' to be listed (fallback)")
	}
	// 4. Bad with Good Logo.jpg -> Present (Because video is now listed)
	if !files["Bad with Good Logo.jpg"] {
		t.Errorf("Expected 'Bad with Good Logo.jpg' to be listed")
	}

	// 5. Good with Bad Logo.mp4 -> Present
	if !files["Good with Bad Logo.mp4"] {
		t.Errorf("Expected 'Good with Bad Logo.mp4' to be listed")
	}
	// 5. Good with Bad Logo.jpg -> Present (Fallback behavior)
	if !files["Good with Bad Logo.jpg"] {
		t.Errorf("Expected 'Good with Bad Logo.jpg' to be listed (fallback)")
	}
}
