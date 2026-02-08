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
	ClearWebDAVCache(hash) // Ensure cache is clear before test
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
		if info.Name() == "My Series - S01 E01.mp4" {
			foundEp1 = true
		}
		if info.Name() == "My Series - S01 E02.mp4" {
			foundEp2 = true
		}
	}
	if !foundEp1 {
		t.Errorf("Season 1 folder did not contain 'My Series - S01 E01.mp4'")
	}
	if !foundEp2 {
		t.Errorf("Season 1 folder did not contain 'My Series - S01 E02.mp4'")
	}

	// Test opening Series File
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Series Group/"+dirSeries+"/My Series/Season 1/My Series - S01 E01.mp4", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open series file: %v", err)
	}
	f.Close()

	// Test VOD with query params (Individual)
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Query Params Group/"+dirIndividual+"/VOD with Query.mp4", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open VOD with query params: %v", err)
	}
	f.Close()

	// Test VOD in Slash Group (Individual)
	f, err = fs.OpenFile(ctx, "/"+hash+"/"+dirOnDemand+"/Slash_Group/"+dirIndividual+"/VOD in Slash Group.mp4", os.O_RDONLY, 0)
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
		{
			input:          "EN_-_The_Queen_s_Gambit__US__S01_E03.mp4",
			expectedName:   "The Queen s Gambit  US",
			expectedSeason: 1,
			expectedOk:     true,
		},
		{
			input:          "The Queen's Gambit - S01E03",
			expectedName:   "The Queen's Gambit",
			expectedSeason: 1,
			expectedOk:     true,
		},
	}

	for _, tc := range testCases {
		name, _, season, ok := parseSeries(tc.input)
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

func TestWebDAVFS_FilenameSanitization(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "xteve_webdav_filename_test")
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

	hash := "testhash_filename"
	ClearWebDAVCache(hash)
	Settings.Files.M3U[hash] = map[string]interface{}{"name": "Test Playlist"}

	// Create dummy M3U file to satisfy stat checks
	err = os.WriteFile(filepath.Join(tempDir, hash+".m3u"), []byte("#EXTM3U"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// This replicates the user's issue and ensures correct formatting
	Data.Streams.All = []interface{}{
		// Case 1: Missing Separator (Fix required)
		// m3u entry: #EXTINF:-1 ... group-title="AMAZON SERIES",AMZ - As We See It S01 E01
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "AMAZON SERIES",
			"name":         "AMZ - As We See It S01 E01",
			"url":          "http://example.com/stream.mp4",
			"_duration":    "3600", // VOD
		},
		// Case 2: Existing Separator (Should remain unchanged)
		// m3u entry: #EXTINF:-1 ... group-title="EXISTING SEPARATOR",My Show - S02 E05 - Episode Name
		map[string]string{
			"_file.m3u.id": hash,
			"group-title":  "EXISTING SEPARATOR",
			"name":         "My Show - S02 E05 - Episode Name",
			"url":          "http://example.com/stream2.mkv",
			"_duration":    "3600", // VOD
		},
	}

	fs := &WebDAVFS{}
	ctx := context.Background()

	// Case 1 Check
	// /dav/<hash>/On Demand/AMAZON SERIES/Series/As We See It/Season 1
	path1 := "/" + hash + "/" + dirOnDemand + "/AMAZON SERIES/" + dirSeries + "/As We See It/Season 1"
	f1, err := fs.OpenFile(ctx, path1, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open season dir 1: %v", err)
	}
	infos1, err := f1.Readdir(-1)
	f1.Close()
	if err != nil {
		t.Fatalf("Failed to read dir 1: %v", err)
	}
	if len(infos1) != 1 {
		t.Fatalf("Expected 1 file in dir 1, got %d", len(infos1))
	}
	actualName1 := infos1[0].Name()
	expectedName1 := "As We See It - S01 E01.mp4"
	if actualName1 != expectedName1 {
		t.Errorf("Case 1 Filename mismatch.\nExpected: %s\nActual:   %s", expectedName1, actualName1)
	} else {
		t.Logf("Case 1 Filename matched: %s", actualName1)
	}

	// Case 2 Check
	// /dav/<hash>/On Demand/EXISTING SEPARATOR/Series/My Show/Season 2
	path2 := "/" + hash + "/" + dirOnDemand + "/EXISTING SEPARATOR/" + dirSeries + "/My Show/Season 2"
	f2, err := fs.OpenFile(ctx, path2, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("Failed to open season dir 2: %v", err)
	}
	infos2, err := f2.Readdir(-1)
	f2.Close()
	if err != nil {
		t.Fatalf("Failed to read dir 2: %v", err)
	}
	if len(infos2) != 1 {
		t.Fatalf("Expected 1 file in dir 2, got %d", len(infos2))
	}
	actualName2 := infos2[0].Name()
	expectedName2 := "My Show - S02 E05 - Episode Name.mkv"
	if actualName2 != expectedName2 {
		t.Errorf("Case 2 Filename mismatch.\nExpected: %s\nActual:   %s", expectedName2, actualName2)
	} else {
		t.Logf("Case 2 Filename matched: %s", actualName2)
	}
}

func TestSeriesRegex_DotSeparator(t *testing.T) {
	testCases := []struct {
		input          string
		expectedName   string
		expectedSeason int
		expectedOk     bool
	}{
		{
			input:          "Name.S01.E01.mp4",
			expectedName:   "Name",
			expectedSeason: 1,
			expectedOk:     true,
		},
		{
			input:          "My.Series.S02E05.mkv",
			expectedName:   "My.Series",
			expectedSeason: 2,
			expectedOk:     true,
		},
		{
			input:          "Another_Show.S10.E01.avi",
			expectedName:   "Another_Show",
			expectedSeason: 10,
			expectedOk:     true,
		},
	}

	for _, tc := range testCases {
		name, _, season, ok := parseSeries(tc.input)
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
