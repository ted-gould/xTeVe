package src

import (
	"context"
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
	origStreamsAll := Data.Streams.All
	defer func() {
		System.Folder.Data = origFolderData
		Settings.Files.M3U = origFilesM3U
		Data.Streams.All = origStreamsAll
	}()

	System.Folder.Data = tempDir
	Settings.Files.M3U = make(map[string]interface{})

	// Create a dummy M3U file
	hash := "testhash"
	content := "#EXTM3U\n#EXTINF:-1 group-title=\"Test Group\",Test Stream\nhttp://test.com/stream.mp4"
	err = os.WriteFile(filepath.Join(tempDir, hash+".m3u"), []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}
	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Test Playlist"}

	// Populate Data.Streams.All manually for "On Demand" testing
	Data.Streams.All = []interface{}{
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Test Group",
			"name":         "Test Stream",
			"url":          "http://test.com/stream.mp4",
			"_duration":    "123", // VOD
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "",
			"name":         "Uncategorized Stream",
			"url":          "http://test.com/other.mp4",
			// No duration, but mp4 extension -> VOD
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Series Group",
			"name":         "My Series S01 E01",
			"url":          "http://test.com/series_s01e01.mp4",
			"_duration":    "123",
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Series Group",
			"name":         "My Series S01 E02",
			"url":          "http://test.com/series_s01e02.mp4",
			"_duration":    "123",
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Mixed Group",
			"name":         "Mixed Series S01 E01",
			"url":          "http://test.com/mixed_series.mp4",
			"_duration":    "123",
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Mixed Group",
			"name":         "Mixed Individual",
			"url":          "http://test.com/mixed_individual.mp4",
			"_duration":    "123",
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Query Params Group",
			"name":         "VOD with Query",
			"url":          "http://test.com/movie.mp4?token=123",
		},
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "Slash/Group",
			"name":         "VOD in Slash Group",
			"url":          "http://test.com/slash.mp4",
		},
	}

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
	f.Close()

	foundHash := false
	for _, info := range infos {
		if info.Name() == hash {
			foundHash = true
			break
		}
	}
	if !foundHash {
		t.Errorf("Root listing did not contain hash directory %s", hash)
	}

	// Test On Demand listing
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open On Demand dir: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read On Demand dir: %v", err)
	}
	f.Close()

	foundGroup := false
	foundSeriesGroup := false
	foundMixedGroup := false
	for _, info := range infos {
		if info.Name() == "Test Group" && info.IsDir() {
			foundGroup = true
		}
		if info.Name() == "Series Group" && info.IsDir() {
			foundSeriesGroup = true
		}
		if info.Name() == "Mixed Group" && info.IsDir() {
			foundMixedGroup = true
		}
	}
	if !foundGroup {
		t.Errorf("On Demand listing did not contain 'Test Group'")
	}
	if !foundSeriesGroup {
		t.Errorf("On Demand listing did not contain 'Series Group'")
	}
	if !foundMixedGroup {
		t.Errorf("On Demand listing did not contain 'Mixed Group'")
	}

	// Test Individual Group Listing (Test Group should have Individual but no Series)
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Test Group", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open Test Group dir: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read Test Group dir: %v", err)
	}
	f.Close()

	foundIndividual := false
	foundSeries := false
	for _, info := range infos {
		if info.Name() == dirIndividual {
			foundIndividual = true
		}
		if info.Name() == dirSeries {
			foundSeries = true
		}
	}
	if !foundIndividual {
		t.Errorf("Test Group did not contain Individual folder")
	}
	if foundSeries {
		t.Errorf("Test Group contained Series folder but shouldn't")
	}

	// Test Series Group Listing
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Series Group", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open Series Group dir: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read Series Group dir: %v", err)
	}
	f.Close()

	foundIndividual = false
	foundSeries = false
	for _, info := range infos {
		if info.Name() == dirIndividual {
			foundIndividual = true
		}
		if info.Name() == dirSeries {
			foundSeries = true
		}
	}
	if foundIndividual {
		t.Errorf("Series Group contained Individual folder but shouldn't")
	}
	if !foundSeries {
		t.Errorf("Series Group did not contain Series folder")
	}

	// Test Mixed Group Listing
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Mixed Group", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open Mixed Group dir: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read Mixed Group dir: %v", err)
	}
	f.Close()

	foundIndividual = false
	foundSeries = false
	for _, info := range infos {
		if info.Name() == dirIndividual {
			foundIndividual = true
		}
		if info.Name() == dirSeries {
			foundSeries = true
		}
	}
	if !foundIndividual {
		t.Errorf("Mixed Group did not contain Individual folder")
	}
	if !foundSeries {
		t.Errorf("Mixed Group did not contain Series folder")
	}

	// Test Browsing Series
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Series Group/"+dirSeries, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open Series folder: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read Series folder: %v", err)
	}
	f.Close()
	foundMySeries := false
	for _, info := range infos {
		if info.Name() == "My Series" {
			foundMySeries = true
		}
	}
	if !foundMySeries {
		t.Errorf("Series folder did not contain 'My Series'")
	}

	// Test Browsing Series Name (Seasons)
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Series Group/"+dirSeries+"/My Series", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open My Series folder: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read My Series folder: %v", err)
	}
	f.Close()
	foundSeason1 := false
	for _, info := range infos {
		if info.Name() == "Season 1" {
			foundSeason1 = true
		}
	}
	if !foundSeason1 {
		t.Errorf("My Series folder did not contain 'Season 1'")
	}

	// Test Browsing Season (Files)
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Series Group/"+dirSeries+"/My Series/Season 1", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open Season 1 folder: %v", err)
	}
	infos, err = f.Readdir(-1)
	if err != nil {
		t.Fatalf("Failed to read Season 1 folder: %v", err)
	}
	f.Close()
	foundEp1 := false
	foundEp2 := false
	for _, info := range infos {
		if info.Name() == "My_Series_S01_E01.mp4" {
			foundEp1 = true
		}
		if info.Name() == "My_Series_S01_E02.mp4" {
			foundEp2 = true
		}
	}
	if !foundEp1 {
		t.Errorf("Season 1 folder did not contain 'My_Series_S01_E01.mp4'")
	}
	if !foundEp2 {
		t.Errorf("Season 1 folder did not contain 'My_Series_S01_E02.mp4'")
	}

	// Test opening Series File
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Series Group/"+dirSeries+"/My Series/Season 1/My_Series_S01_E01.mp4", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open series file: %v", err)
	}
	f.Close()

	// Test VOD with query params (Individual)
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Query Params Group/"+dirIndividual+"/VOD_with_Query.mp4", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open VOD with query params: %v", err)
	}
	f.Close()

	// Test VOD in Slash Group (Individual)
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Slash_Group/"+dirIndividual+"/VOD_in_Slash_Group.mp4", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open VOD in Slash Group: %v", err)
	}
	f.Close()
}

func TestParseSeriesUserScenario(t *testing.T) {
	testCases := []struct {
		input          string
		expectedName   string
		expectedSeason int
		expectedOk     bool
	}{
		{
			input:          "text_-_Foo_Bar_S01_E01",
			expectedName:   "Foo Bar",
			expectedSeason: 1,
			expectedOk:     true,
		},
		{
			input:          "text_-_Foo_Bar_S01_E01.mkv",
			expectedName:   "Foo Bar",
			expectedSeason: 1,
			expectedOk:     true,
		},
		// Existing cases should still work
		{
			input:          "My Series S01 E01",
			expectedName:   "My Series",
			expectedSeason: 1,
			expectedOk:     true,
		},
		{
			input:          "Prefix - My Series S01 E01",
			expectedName:   "My Series",
			expectedSeason: 1,
			expectedOk:     true,
		},
		// Case insensitive
		{
			input:          "text_-_foo_bar_s02_e05",
			expectedName:   "foo bar",
			expectedSeason: 2,
			expectedOk:     true,
		},
	}

	for _, tc := range testCases {
		name, season, ok := parseSeries(tc.input)
		if ok != tc.expectedOk {
			t.Errorf("Input: %s, Expected ok: %v, got: %v", tc.input, tc.expectedOk, ok)
		}
		if ok {
			if name != tc.expectedName {
				t.Errorf("Input: %s, Expected name: '%s', got: '%s'", tc.input, tc.expectedName, name)
			}
			if season != tc.expectedSeason {
				t.Errorf("Input: %s, Expected season: %d, got: %d", tc.input, tc.expectedSeason, season)
			}
		}
	}
}
