package src

import (
	"context"
	"os"
	"testing"
	"xteve/src/internal/imgcache"

	"github.com/stretchr/testify/assert"
)

func TestFilterThisStream_GroupTitle_Bug(t *testing.T) {
	// This test demonstrates the bug.
	// A stream with group-title="News" and name="National Report"
	// should be matched by a filter on group-title="News" with an include condition of "{News}".
	// The bug causes this to fail because the include condition is checked against the stream name instead of the group title.

	// Setup: Create a stream
	stream := map[string]string{
		"name":        "National Report",
		"group-title": "News",
		"_values":     "National Report News",
	}

	// Setup: Create a filter
	filter := Filter{
		Type:          "group-title",
		Rule:          "News {News}",
		CaseSensitive: false,
	}

	// Setup: Reset and populate Data.Filter
	Data.Filter = []Filter{filter}

	// Execute
	result := FilterThisStream(stream)

	// Assert: This will fail before the fix
	assert.True(t, result, "Stream should be matched by the filter")
}

func TestFilterThisStream_CustomFilter(t *testing.T) {
	// This test ensures that the custom-filter functionality is not broken.

	// Setup: Create a stream
	stream := map[string]string{
		"name":        "Some Channel",
		"group-title": "Some Group",
		"_values":     "Some Channel Some Group keyword",
	}

	// Setup: Create a filter
	filter := Filter{
		Type:          "custom-filter",
		Rule:          "keyword",
		CaseSensitive: false,
	}
	Data.Filter = []Filter{filter}

	// Execute
	result := FilterThisStream(stream)

	// Assert
	assert.True(t, result, "Stream should be matched by the custom filter")
}

func TestFilterThisStream_GroupTitle_SpecialCharacters(t *testing.T) {
	// This test ensures that group-title filters with special characters are handled correctly.

	// Setup: Create a stream
	stream := map[string]string{
		"name":        "Some Channel",
		"group-title": "!@#$%^&*()_+-=[]{};':\",./<>?",
		"_values":     "Some Channel !@#$%^&*()_+-=[]{};':\",./<>?",
	}

	// Setup: Create a filter
	filter := Filter{
		Type:          "group-title",
		Rule:          "!@#$%^&*()_+-=[]{};':\",./<>?",
		CaseSensitive: true,
	}
	Data.Filter = []Filter{filter}

	// Execute
	result := FilterThisStream(stream)

	// Assert
	assert.True(t, result, "Stream should be matched by the filter with special characters")
}

func TestFilterThisStream_GroupTitle_UnicodeCharacters(t *testing.T) {
	// This test ensures that group-title filters with unicode characters are handled correctly.

	// Setup: Create a stream
	stream := map[string]string{
		"name":        "Some Channel",
		"group-title": "뉴스", // "News" in Korean
		"_values":     "Some Channel 뉴스",
	}

	// Setup: Create a filter
	filter := Filter{
		Type:          "group-title",
		Rule:          "뉴스",
		CaseSensitive: false,
	}
	Data.Filter = []Filter{filter}

	// Execute
	result := FilterThisStream(stream)

	// Assert
	assert.True(t, result, "Stream should be matched by the filter with unicode characters")
}

func TestFilterThisStream_ExcludeExactPhrase(t *testing.T) {
	// This test ensures that excluding a specific phrase does not also exclude a stream that contains a substring of that phrase.
	// For example, excluding "CSPAN 2" should not exclude "CSPAN".

	// Setup: Create streams
	streamToKeep := map[string]string{
		"name":        "CSPAN",
		"group-title": "News",
		"_values":     "CSPAN News",
	}
	streamToExclude := map[string]string{
		"name":        "CSPAN 2",
		"group-title": "News",
		"_values":     "CSPAN 2 News",
	}

	// Setup: Create a filter to exclude "CSPAN 2" from the "News" group
	filter := Filter{
		Type:          "group-title",
		Rule:          "News !{CSPAN 2}",
		CaseSensitive: false,
	}
	Data.Filter = []Filter{filter}

	// Execute and Assert
	assert.True(t, FilterThisStream(streamToKeep), "CSPAN should be kept")
	assert.False(t, FilterThisStream(streamToExclude), "CSPAN 2 should be excluded")
}

func TestBuildM3U_PMSSource(t *testing.T) {
	// Setup: Set EPG source to PMS
	Settings.EpgSource = "PMS"
	// Restore original settings after test
	defer func() {
		Settings.EpgSource = "XEPG"
	}()

	// Setup: Create mock active streams
	stream1 := map[string]string{
		"name":         "Channel 1",
		"group-title":  "Group 1",
		"url":          "http://test.com/stream1",
		"_file.m3u.id": "test_m3u_id",
		"tvg-logo":     "logo1.png",
		"tvg-id":       "channel1.tv",
	}
	Data.Streams.Active = []any{stream1}
	defer func() {
		Data.Streams.Active = make([]any, 0)
	}()

	// Setup: System variables needed for URL generation
	System.ServerProtocol.WEB = "http"
	System.ServerProtocol.XML = "http"
	System.ServerProtocol.DVR = "http"
	System.Domain = "localhost:34400"
	System.Folder.Data = "" // To avoid writing file

	// Setup: Initialize caches
	Data.Cache.StreamingURLS = make(map[string]StreamInfo)
	var err error
	// Create a temporary directory for the image cache
	tempDir, err := os.MkdirTemp("", "xteve-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	Data.Cache.Images, err = imgcache.New(tempDir, "", false)
	assert.NoError(t, err)

	// Execute
	m3u, err := buildM3U(context.Background(), []string{})

	// Assert
	assert.NoError(t, err)
	assert.NotEmpty(t, m3u)
	// The failing assertion:
	assert.Contains(t, m3u, `tvg-name="Channel 1"`, "M3U should contain channel 1")
}
