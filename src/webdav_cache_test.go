package src

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWebDAVCacheRefresh(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "xteve_webdav_cache_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Save original values
	origFolderData := System.Folder.Data
	origFilesM3U := Settings.Files.M3U
	origStreamsAll := Data.Streams.All
	defer func() {
		System.Folder.Data = origFolderData
		Settings.Files.M3U = origFilesM3U
		Data.Streams.All = origStreamsAll
		// Clean up global cache
		ClearWebDAVCache("")
	}()

	System.Folder.Data = tempDir
	Settings.Files.M3U = make(map[string]interface{})

	// Create a dummy M3U file
	hash := "testhash_cache"
	content := "#EXTM3U\n#EXTINF:-1 group-title=\"Group A\",Stream A\nhttp://test.com/a.mp4"
	err = os.WriteFile(filepath.Join(tempDir, hash+".m3u"), []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Test Cache Playlist"}

	// Populate Data.Streams.All manually
	Data.Streams.All = []interface{}{
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Group A",
			"name":         "Stream A",
			"url":          "http://test.com/a.mp4",
			"_duration":    "123", // VOD
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Group A",
			"name":         "My Series S01 E01",
			"url":          "http://test.com/s01e01.mp4",
			"_duration":    "123", // VOD
		},
	}

	fs := &WebDAVFS{}
	ctx := context.Background()

	// 1. Access WebDAV groups to populate the Groups cache.
	f, err := fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open On Demand dir: %v", err)
	}
	// Use blank identifier as we only care about the side-effect of caching
	_, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read On Demand dir: %v", err)
	}
	f.Close()

	// Verify Group Cache
	webdavCacheMutex.RLock()
	hc, ok := webdavCache[hash]
	webdavCacheMutex.RUnlock()
	if !ok || hc.Groups == nil {
		t.Errorf("Groups cache was not populated")
	} else if len(hc.Groups) != 1 || hc.Groups[0] != "Group A" {
		t.Errorf("Groups cache content incorrect: %v", hc.Groups)
	}

	// 2. Access Series to populate Series Cache
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Group A/"+dirSeries, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open Series dir: %v", err)
	}
	_, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read Series dir: %v", err)
	}
	f.Close()

	// Verify Series Cache
	webdavCacheMutex.RLock()
	hc, ok = webdavCache[hash]
	webdavCacheMutex.RUnlock()
	if !ok || hc.Series == nil {
		t.Errorf("Series cache was not populated")
	} else {
		if list, ok := hc.Series["Group A"]; !ok {
			t.Errorf("Series cache missing Group A")
		} else if len(list) != 1 || list[0] != "My Series" {
			t.Errorf("Series cache content incorrect: %v", list)
		}
	}

	// 3. Access Seasons to populate Seasons Cache
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Group A/"+dirSeries+"/My Series", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open My Series dir: %v", err)
	}
	_, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read My Series dir: %v", err)
	}
	f.Close()

	// Verify Seasons Cache
	webdavCacheMutex.RLock()
	hc, ok = webdavCache[hash]
	webdavCacheMutex.RUnlock()
	if !ok || hc.Seasons == nil {
		t.Errorf("Seasons cache was not populated")
	} else {
		key := seasonKey{Group: "Group A", Series: "My Series"}
		if list, ok := hc.Seasons[key]; !ok {
			t.Errorf("Seasons cache missing key: %v", key)
		} else if len(list) != 1 || list[0] != "Season 1" {
			t.Errorf("Seasons cache content incorrect: %v", list)
		}
	}

	// 4. Access Files to populate SeasonFiles Cache
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Group A/"+dirSeries+"/My Series/Season 1", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open Season 1 dir: %v", err)
	}
	_, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read Season 1 dir: %v", err)
	}
	f.Close()

	// Verify SeasonFiles Cache
	webdavCacheMutex.RLock()
	hc, ok = webdavCache[hash]
	webdavCacheMutex.RUnlock()
	if !ok || hc.SeasonFiles == nil {
		t.Errorf("SeasonFiles cache was not populated")
	} else {
		key := seasonFileKey{Group: "Group A", Series: "My Series", Season: "Season 1"}
		if list, ok := hc.SeasonFiles[key]; !ok {
			t.Errorf("SeasonFiles cache missing key: %v", key)
		} else if len(list) != 1 {
			t.Errorf("SeasonFiles cache content incorrect: %v", list)
		} else if list[0].Name == "" {
			t.Errorf("SeasonFiles cache content incorrect (empty name)")
		}
	}

	// 5. Access Individual to populate IndividualFiles Cache
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Group A/"+dirIndividual, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open Individual dir: %v", err)
	}
	_, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read Individual dir: %v", err)
	}
	f.Close()

	// Verify IndividualFiles Cache
	webdavCacheMutex.RLock()
	hc, ok = webdavCache[hash]
	webdavCacheMutex.RUnlock()
	if !ok || hc.IndividualFiles == nil {
		t.Errorf("IndividualFiles cache was not populated")
	} else {
		if list, ok := hc.IndividualFiles["Group A"]; !ok {
			t.Errorf("IndividualFiles cache missing Group A")
		} else if len(list) != 1 || list[0].Name != "Stream A.mp4" {
			t.Errorf("IndividualFiles cache content incorrect: %v", list)
		}
	}

	// 6. Call ClearWebDAVCache("").
	ClearWebDAVCache("")

	// Verify cache is cleared
	webdavCacheMutex.RLock()
	_, ok = webdavCache[hash]
	webdavCacheMutex.RUnlock()
	if ok {
		t.Errorf("Cache was not cleared")
	}

	// 7. Verify refetch works (smoke test)
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open On Demand dir after clear: %v", err)
	}
	infos, err := f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read On Demand dir after clear: %v", err)
	}
	f.Close()
	if len(infos) == 0 {
		t.Errorf("Refetch returned empty list")
	}
}
