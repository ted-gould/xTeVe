package src

import (
	"fmt"
	"os"
	"testing"
)

// TestCleanupXEPG verifies the cleanupXEPG function logic.
// It sets up a mock Data and Settings state and checks if channels are correctly removed or kept.
func TestCleanupXEPG(t *testing.T) {
	// Setup global state
	originalSystem := System
	originalSettings := Settings
	originalData := Data

	// Teardown
	defer func() {
		System = originalSystem
		Settings = originalSettings
		Data = originalData
		os.Remove(System.File.XEPG)
	}()

	// Mock System and File paths
	System.File.XEPG = "./test_xepg_cleanup.json"

	// Mock Data
	Data.XEPG.Channels = make(map[string]XEPGChannelStruct)
	Data.Cache.Streams.Active = []string{}
	Data.XEPG.XEPGCount = 0

	// Mock Settings
	Settings.Files.M3U = make(map[string]any)
	Settings.Files.HDHR = make(map[string]any)

	// --- Scenarios ---

	// 1. Keep: Active stream, Source exists, Active
	// ID: "keep_1", Name: "Channel1", FileM3UID: "source1"
	Data.XEPG.Channels["keep_1"] = XEPGChannelStruct{
		Name:      "Channel1",
		FileM3UID: "source1",
		XActive:   true,
	}
	Data.Cache.Streams.Active = append(Data.Cache.Streams.Active, "Channel1"+"source1")
	Settings.Files.M3U["source1"] = "some info"

	// 2. Delete: Not in active streams
	// ID: "del_1", Name: "Channel2", FileM3UID: "source1"
	Data.XEPG.Channels["del_1"] = XEPGChannelStruct{
		Name:      "Channel2",
		FileM3UID: "source1",
		XActive:   true,
	}
	// Missing from Data.Cache.Streams.Active

	// 3. Delete: Source does not exist
	// ID: "del_2", Name: "Channel3", FileM3UID: "source2"
	Data.XEPG.Channels["del_2"] = XEPGChannelStruct{
		Name:      "Channel3",
		FileM3UID: "source2",
		XActive:   true,
	}
	Data.Cache.Streams.Active = append(Data.Cache.Streams.Active, "Channel3"+"source2")
	// Missing from Settings.Files.M3U/HDHR

	// 4. Keep: Inactive (XActive=false), but should still be in map if other conditions met
	// ID: "keep_2", Name: "Channel4", FileM3UID: "source1"
	Data.XEPG.Channels["keep_2"] = XEPGChannelStruct{
		Name:      "Channel4",
		FileM3UID: "source1",
		XActive:   false,
	}
	Data.Cache.Streams.Active = append(Data.Cache.Streams.Active, "Channel4"+"source1")

	// Run cleanup
	cleanupXEPG()

	// Verify
	if _, ok := Data.XEPG.Channels["keep_1"]; !ok {
		t.Error("Channel keep_1 should have been kept")
	}

	if _, ok := Data.XEPG.Channels["del_1"]; ok {
		t.Error("Channel del_1 should have been deleted (not in active streams)")
	}

	if _, ok := Data.XEPG.Channels["del_2"]; ok {
		t.Error("Channel del_2 should have been deleted (source not found)")
	}

	if _, ok := Data.XEPG.Channels["keep_2"]; !ok {
		t.Error("Channel keep_2 should have been kept")
	}

	// Check XEPGCount
	// keep_1 is XActive=true -> Count 1
	// keep_2 is XActive=false -> Count 0 contribution
	// Total expected: 1
	expectedCount := int64(1)
	if Data.XEPG.XEPGCount != expectedCount {
		t.Errorf("Expected XEPGCount to be %d, got %d", expectedCount, Data.XEPG.XEPGCount)
	}

	fmt.Printf("Data.XEPG.Channels len: %d\n", len(Data.XEPG.Channels))
}
