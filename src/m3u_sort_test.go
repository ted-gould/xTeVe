package src

import (
	"os"
	"strings"
	"testing"
	"xteve/src/internal/imgcache"
)

func TestBuildM3U_Sorting(t *testing.T) {
	// Setup: Set EPG source to XEPG
	Settings.EpgSource = "XEPG"
	Data.XEPG.Channels = make(map[string]XEPGChannelStruct)

	// Add channels with specific channel numbers in random order
	Data.XEPG.Channels["id1"] = XEPGChannelStruct{
		XActive:     true,
		XChannelID:  "10.0",
		XName:       "Channel 10",
		XGroupTitle: "Group",
		XEPG:        "id1",
	}
	Data.XEPG.Channels["id2"] = XEPGChannelStruct{
		XActive:     true,
		XChannelID:  "2.0",
		XName:       "Channel 2",
		XGroupTitle: "Group",
		XEPG:        "id2",
	}
	Data.XEPG.Channels["id3"] = XEPGChannelStruct{
		XActive:     true,
		XChannelID:  "5.5",
		XName:       "Channel 5.5",
		XGroupTitle: "Group",
		XEPG:        "id3",
	}

	defer func() {
		Settings.EpgSource = "PMS" // Reset to default or previous state if needed
		Data.XEPG.Channels = make(map[string]XEPGChannelStruct)
	}()

	// Setup: System variables needed
	System.ServerProtocol.WEB = "http"
	System.ServerProtocol.XML = "http"
	System.ServerProtocol.DVR = "http"
	System.Domain = "localhost:34400"
	System.Folder.Data = ""

	// Setup: Initialize caches
	Data.Cache.StreamingURLS = make(map[string]StreamInfo)
	var err error
	tempDir, err := os.MkdirTemp("", "xteve-test-sort-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	Data.Cache.Images, err = imgcache.New(tempDir, "", false, NewHTTPClient())
	if err != nil {
		t.Fatalf("Failed to init imgcache: %v", err)
	}

	// Execute
	m3u, err := buildM3U([]string{})
	if err != nil {
		t.Fatalf("buildM3U failed: %v", err)
	}

	// Assert order
	// We expect Channel 2, then Channel 5.5, then Channel 10
	idx2 := strings.Index(m3u, `tvg-name="Channel 2"`)
	idx5 := strings.Index(m3u, `tvg-name="Channel 5.5"`)
	idx10 := strings.Index(m3u, `tvg-name="Channel 10"`)

	if idx2 == -1 {
		t.Error("Channel 2 missing")
	}
	if idx5 == -1 {
		t.Error("Channel 5.5 missing")
	}
	if idx10 == -1 {
		t.Error("Channel 10 missing")
	}

	if idx2 >= idx5 {
		t.Errorf("Order incorrect: Channel 2 (idx %d) should be before Channel 5.5 (idx %d)", idx2, idx5)
	}
	if idx5 >= idx10 {
		t.Errorf("Order incorrect: Channel 5.5 (idx %d) should be before Channel 10 (idx %d)", idx5, idx10)
	}
}
