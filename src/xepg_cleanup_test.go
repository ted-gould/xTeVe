package src

import (
	"path/filepath"
	"testing"
)

func TestCleanupXEPG(t *testing.T) {
	// Setup global state
	originalData := Data
	originalSettings := Settings
	originalSystem := System
	defer func() {
		Data = originalData
		Settings = originalSettings
		System = originalSystem
	}()

	// Mock System File Path
	System.File.XEPG = filepath.Join(t.TempDir(), "xepg.json")

	// Mock Data
	Data.XEPG.Channels = make(map[string]XEPGChannelStruct)
	Data.Cache.Streams.Active = []string{}
	Settings.Files.M3U = make(map[string]interface{})
	Settings.Files.HDHR = make(map[string]interface{})

	// Add mock channels
	// 1. Valid channel (Active stream, Valid Source ID)
	Data.XEPG.Channels["valid"] = XEPGChannelStruct{
		Name:      "ValidChannel",
		FileM3UID: "source1",
		XActive:   true,
	}
	Data.Cache.Streams.Active = append(Data.Cache.Streams.Active, "ValidChannelsource1")
	Settings.Files.M3U["source1"] = nil

	// 2. Inactive Stream (Should be deleted)
	Data.XEPG.Channels["inactiveStream"] = XEPGChannelStruct{
		Name:      "InactiveStream",
		FileM3UID: "source1",
		XActive:   true,
	}
	// Not adding to Data.Cache.Streams.Active

	// 3. Invalid Source ID (Should be deleted)
	Data.XEPG.Channels["invalidSource"] = XEPGChannelStruct{
		Name:      "InvalidSource",
		FileM3UID: "source2",
		XActive:   true,
	}
	Data.Cache.Streams.Active = append(Data.Cache.Streams.Active, "InvalidSourcesource2")
	// Not adding source2 to Settings.Files.M3U

	// 4. Valid but Inactive (XActive=false) - Should be kept but not counted
	Data.XEPG.Channels["validInactive"] = XEPGChannelStruct{
		Name:      "ValidInactive",
		FileM3UID: "source1",
		XActive:   false,
	}
	Data.Cache.Streams.Active = append(Data.Cache.Streams.Active, "ValidInactivesource1")

	// Run cleanup
	cleanupXEPG()

	// Assertions
	if _, ok := Data.XEPG.Channels["valid"]; !ok {
		t.Error("Valid channel was deleted")
	}
	if _, ok := Data.XEPG.Channels["inactiveStream"]; ok {
		t.Error("Channel with inactive stream was not deleted")
	}
	if _, ok := Data.XEPG.Channels["invalidSource"]; ok {
		t.Error("Channel with invalid source was not deleted")
	}
	if _, ok := Data.XEPG.Channels["validInactive"]; !ok {
		t.Error("Valid but inactive channel was deleted")
	}

	if Data.XEPG.XEPGCount != 1 {
		t.Errorf("Expected XEPGCount to be 1, got %d", Data.XEPG.XEPGCount)
	}
}
