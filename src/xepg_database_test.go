package src

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/samber/lo"
	// "strings" // Removed as it's unused now
)

// testSystem, testSettings, testData are mock versions for testing
// Initialize with minimal necessary fields matching actual struct definitions
var testSystem = SystemStruct{
	Folder: struct { // src.SystemStruct.Folder
		Backup       string
		Cache        string
		Certificates string
		Config       string
		Data         string
		ImagesCache  string
		ImagesUpload string
		Temp         string
	}{
		Data: "./testdata/", // Only this field is used by the functions under test via System.File.XEPG path construction indirectly
	},
	File: struct { // src.SystemStruct.File
		Authentication    string
		M3U               string
		PMS               string
		ServerCert        string
		ServerCertPrivKey string
		Settings          string
		URLS              string
		XEPG              string
		XML               string
	}{
		XEPG: "./testdata/xepg.json", // Used by load/save functions
	},
}

var testSettings = SettingsStruct{ // src.SettingsStruct
	MappingFirstChannel: 1000, // Used by findFreeChannelNumber
}

// Helper function to set up global state for a test
func setupGlobalStateForTest() func() {
	originalSystem := System
	originalSettings := Settings
	originalData := Data

	System = testSystem
	Settings = testSettings

	// Initialize Data with a fresh DataStruct structure for each test.
	var freshData DataStruct
	freshData.XEPG.Channels = make(map[string]any)
	// The following map initializations are important if the functions under test
	// (or helpers they call like loadJSONFileToMap) expect non-nil maps.
	// loadJSONFileToMap in the original code initializes Data.Cache.XMLTV if it's nil,
	// but it's safer to initialize it here.
	freshData.Cache.XMLTV = make(map[string]XMLTV)
	// freshData.Cache.Images remains nil (*imgcache.Cache zero value), which is acceptable
	// as the tested functions do not directly use Data.Cache.Images.
	Data = freshData

	os.MkdirAll(System.Folder.Data, os.ModePerm)
	os.Remove(System.File.XEPG) // Clean up from previous tests

	return func() { // Teardown function
		System = originalSystem
		Settings = originalSettings
		Data = originalData // Restore original global Data
		os.Remove(System.File.XEPG)
	}
}

func TestGenerateNewXEPGID(t *testing.T) {
	teardown := setupGlobalStateForTest()
	defer teardown()

	// Test sequence by explicitly adding to Data.XEPG.Channels
	Data.XEPG.Channels = make(map[string]any) // Ensure clean start for this test
	expectedIDs := []string{"x-ID.0", "x-ID.1", "x-ID.2"}
	for _, expectedID := range expectedIDs {
		id := generateNewXEPGID()
		if id != expectedID {
			t.Errorf("Expected ID %s, got %s", expectedID, id)
		}
		// Simulate the caller adding the new channel (and its ID) to the map
		// This is crucial because generateNewXEPGID checks Data.XEPG.Channels
		Data.XEPG.Channels[id] = XEPGChannelStruct{XEPG: id} // Add dummy struct
	}

	// Test that it reuses IDs if one is "freed" (not standard, but tests loop)
	// This part of the test might be misleading as the current generateNewXEPGID always increments.
	// For now, focusing on sequential generation.
	// delete(Data.XEPG.Channels, "x-ID.1")
	// id_reuse := generateNewXEPGID()
	// if id_reuse != "x-ID.1" {
	//  t.Errorf("Expected ID x-ID.1 to be reused, got %s", id_reuse)
	// }
}

func TestFindFreeChannelNumber(t *testing.T) {
	teardown := setupGlobalStateForTest()
	defer teardown()

	allChannelNumbers := []float64{}
	Settings.MappingFirstChannel = 1000 // Ensure testSettings is used via System

	// Test 1: Empty allChannelNumbers
	ch1 := findFreeChannelNumber(&allChannelNumbers)
	if ch1 != "1000" {
		t.Errorf("Expected channel number 1000, got %s", ch1)
	}
	if !lo.Contains(allChannelNumbers, 1000.0) {
		t.Errorf("Expected 1000 to be added to allChannelNumbers. Got: %v", allChannelNumbers)
	}

	// Test 2: With existing numbers
	allChannelNumbers = []float64{1000, 1001, 1003}
	ch2 := findFreeChannelNumber(&allChannelNumbers)
	if ch2 != "1002" {
		t.Errorf("Expected channel number 1002, got %s", ch2)
	}
	if !lo.Contains(allChannelNumbers, 1002.0) {
		t.Errorf("Expected 1002 to be added to allChannelNumbers. Got: %v", allChannelNumbers)
	}

	// Test 3: With startingChannel hint
	allChannelNumbers = []float64{1000, 1001, 1003, 1002} // unsorted
	ch3 := findFreeChannelNumber(&allChannelNumbers, "1005")
	if ch3 != "1005" {
		t.Errorf("Expected channel number 1005, got %s", ch3)
	}
	if !lo.Contains(allChannelNumbers, 1005.0) {
		t.Errorf("Expected 1005 to be added to allChannelNumbers. Got: %v", allChannelNumbers)
	}

	// Test 4: Starting channel hint is already taken
	allChannelNumbers = []float64{1000, 1001, 1003, 1002, 1005}
	ch4 := findFreeChannelNumber(&allChannelNumbers, "1003") // 1003 is taken, next should be 1004
	if ch4 != "1004" {
		t.Errorf("Expected channel number 1004, got %s", ch4)
	}
	if !lo.Contains(allChannelNumbers, 1004.0) {
		t.Errorf("Expected 1004 to be added to allChannelNumbers. Got: %v", allChannelNumbers)
	}

	// Test 5: Starting channel hint is empty string
	allChannelNumbers = []float64{1000, 1001, 1002, 1003, 1004, 1005}
	Settings.MappingFirstChannel = 1000
	ch5 := findFreeChannelNumber(&allChannelNumbers, "")
	if ch5 != "1006" {
		t.Errorf("Expected channel number 1006 when starting hint is empty, got %s. Numbers: %v", ch5, allChannelNumbers)
	}
	if !lo.Contains(allChannelNumbers, 1006.0) {
		t.Errorf("Expected 1006 to be added to allChannelNumbers. Got: %v", allChannelNumbers)
	}

	// Test 6: Settings.MappingFirstChannel is higher
	allChannelNumbers = []float64{}
	Settings.MappingFirstChannel = 2000
	ch6 := findFreeChannelNumber(&allChannelNumbers)
	if ch6 != "2000" {
		t.Errorf("Expected channel number 2000, got %s", ch6)
	}
	if !lo.Contains(allChannelNumbers, 2000.0) {
		t.Errorf("Expected 2000 to be added to allChannelNumbers. Got: %v", allChannelNumbers)
	}
}

func TestGenerateChannelHash(t *testing.T) {
	// No global state needed for this test, but setup/teardown won't hurt
	teardown := setupGlobalStateForTest()
	defer teardown()

	hash1 := generateChannelHash("m3u1", "Channel1", "Group1", "tvg1", "TvgName1", "uuidKey1", "uuidValue1")
	hashDifferent := generateChannelHash("m3u2", "Channel2", "Group2", "tvg2", "TvgName2", "uuidKey2", "uuidValue2")
	hash3 := generateChannelHash("m3u1", "Channel1", "Group1", "tvg1", "TvgName1", "uuidKey1", "uuidValue1") // Same as hash1

	if hash1 == "" {
		t.Error("Expected hash1 to not be empty")
	}
	if hashDifferent == "" {
		t.Error("Expected hashDifferent to not be empty")
	}
	if hash1 == hashDifferent {
		t.Errorf("Expected hash1 and hashDifferent to be different, got %s and %s", hash1, hashDifferent)
	}
	if hash1 != hash3 {
		t.Errorf("Expected hash1 and hash3 to be the same, got %s and %s", hash1, hash3)
	}
	// md5("m3u1Channel1Group1tvg1TvgName1uuidKey1uuidValue1") is 8b783b0689c25221d4988c8066f4a3a7
	expectedHash := "8b783b0689c25221d4988c8066f4a3a7"
	if hash1 != expectedHash {
		t.Errorf("Expected hash %s, got %s", expectedHash, hash1)
	}
}

func TestProcessExistingXEPGChannel(t *testing.T) {
	teardown := setupGlobalStateForTest()
	defer teardown()

	xepgID := "x-ID.0" // A known ID that will be used for the test channel
	initialChannel := XEPGChannelStruct{
		XEPG:                xepgID,
		Name:                "Old Name",
		URL:                 "http://old.url",
		GroupTitle:          "Old Group",
		TvgLogo:             "old_logo.png",
		XUpdateChannelName:  true,
		XUpdateChannelGroup: true,
		XUpdateChannelIcon:  true,
		XName:               "Old XName",
		XGroupTitle:         "Old XGroupTitle",
	}
	Data.XEPG.Channels[xepgID] = initialChannel

	m3uChannel := M3UChannelStructXEPG{
		Name:       "New Name",
		URL:        "http://new.url",
		GroupTitle: "New Group",
		TvgLogo:    "new_logo.png",
	}

	// Test Case 1: channelHasUUID = true, all updates enabled
	err := processExistingXEPGChannel(m3uChannel, xepgID, true)
	if err != nil {
		t.Fatalf("processExistingXEPGChannel failed: %v", err)
	}

	// Retrieve the updated channel from Data.XEPG.Channels for verification
	updatedChannelData, ok := Data.XEPG.Channels[xepgID]
	if !ok {
		t.Fatalf("Channel with ID %s not found in Data.XEPG.Channels after update", xepgID)
	}
	updatedChannelBytes, _ := json.Marshal(updatedChannelData)
	var updatedChannel XEPGChannelStruct
	json.Unmarshal(updatedChannelBytes, &updatedChannel)

	if updatedChannel.Name != "New Name" {
		t.Errorf("Expected Name to be 'New Name', got '%s'", updatedChannel.Name)
	}
	if updatedChannel.URL != "http://new.url" {
		t.Errorf("Expected URL to be 'http://new.url', got '%s'", updatedChannel.URL)
	}
	if updatedChannel.XName != "New Name" {
		t.Errorf("Expected XName to be 'New Name', got '%s'", updatedChannel.XName)
	}
	if updatedChannel.GroupTitle != "New Group" {
		t.Errorf("Expected GroupTitle to be 'New Group', got '%s'", updatedChannel.GroupTitle)
	}
	if updatedChannel.XGroupTitle != "New Group" {
		t.Errorf("Expected XGroupTitle to be 'New Group', got '%s'", updatedChannel.XGroupTitle)
	}
	if updatedChannel.TvgLogo != "new_logo.png" {
		t.Errorf("Expected TvgLogo to be 'new_logo.png', got '%s'", updatedChannel.TvgLogo)
	}

	// Test Case 2: channelHasUUID = false, XUpdateChannelName should not update XName
	initialChannel.XName = "NoChangeXName"
	initialChannel.XGroupTitle = "Old XGroupTitle"
	initialChannel.TvgLogo = "old_logo.png"
	initialChannel.XUpdateChannelName = true
	Data.XEPG.Channels[xepgID] = initialChannel

	m3uChannel.Name = "NameNoUUID"
	m3uChannel.GroupTitle = "GroupNoUUID"
	m3uChannel.TvgLogo = "LogoNoUUID"

	err = processExistingXEPGChannel(m3uChannel, xepgID, false)
	if err != nil {
		t.Fatalf("processExistingXEPGChannel failed: %v", err)
	}

	updatedChannelData2, ok2 := Data.XEPG.Channels[xepgID]
	if !ok2 {
		t.Fatalf("Channel with ID %s not found after update (Test Case 2)", xepgID)
	}
	updatedChannelBytes2, _ := json.Marshal(updatedChannelData2)
	var updatedChannel2 XEPGChannelStruct
	json.Unmarshal(updatedChannelBytes2, &updatedChannel2)

	if updatedChannel2.Name != "NameNoUUID" {
		t.Errorf("Expected Name to be 'NameNoUUID', got '%s'", updatedChannel2.Name)
	}
	if updatedChannel2.XName != "NoChangeXName" {
		t.Errorf("Expected XName to be 'NoChangeXName', got '%s'", updatedChannel2.XName)
	}
	if updatedChannel2.XGroupTitle != "GroupNoUUID" {
		t.Errorf("Expected XGroupTitle to be 'GroupNoUUID', got '%s'", updatedChannel2.XGroupTitle)
	}
	if updatedChannel2.TvgLogo != "LogoNoUUID" {
		t.Errorf("Expected TvgLogo to be 'LogoNoUUID', got '%s'", updatedChannel2.TvgLogo)
	}

	// Test Case 3: Update flags are false
	initialChannel.XUpdateChannelName = false
	initialChannel.XUpdateChannelGroup = false
	initialChannel.XUpdateChannelIcon = false
	initialChannel.XName = "XNameNoUpdateFlag"
	initialChannel.XGroupTitle = "XGroupTitleNoUpdateFlag"
	initialChannel.TvgLogo = "TvgLogoNoUpdateFlag"
	Data.XEPG.Channels[xepgID] = initialChannel

	m3uChannel.Name = "NameFlagFalse" // These values from m3uChannel should not be reflected in XName, XGroupTitle, TvgLogo
	m3uChannel.GroupTitle = "GroupFlagFalse"
	m3uChannel.TvgLogo = "LogoFlagFalse"

	err = processExistingXEPGChannel(m3uChannel, xepgID, true) // channelHasUUID is true, but flags are false
	if err != nil {
		t.Fatalf("processExistingXEPGChannel failed: %v", err)
	}
	updatedChannelData3 := Data.XEPG.Channels[xepgID]
	updatedChannelBytes3, _ := json.Marshal(updatedChannelData3)
	var updatedChannel3 XEPGChannelStruct
	json.Unmarshal(updatedChannelBytes3, &updatedChannel3)

	if updatedChannel3.XName != "XNameNoUpdateFlag" {
		t.Errorf("Expected XName to be 'XNameNoUpdateFlag', got '%s'", updatedChannel3.XName)
	}
	if updatedChannel3.XGroupTitle != "XGroupTitleNoUpdateFlag" {
		t.Errorf("Expected XGroupTitle to be 'XGroupTitleNoUpdateFlag', got '%s'", updatedChannel3.XGroupTitle)
	}
	if updatedChannel3.TvgLogo != "TvgLogoNoUpdateFlag" { // TvgLogo is from m3u if XUpdateChannelIcon is false
		t.Errorf("Expected TvgLogo to be 'TvgLogoNoUpdateFlag', got '%s'", updatedChannel3.TvgLogo)
	}
}

func TestProcessNewXEPGChannel(t *testing.T) {
	teardown := setupGlobalStateForTest()
	defer teardown()

	allChannelNumbers := []float64{}
	Settings.MappingFirstChannel = 2000

	valuesMap := map[string]string{"attr1": "val1"}
	valuesJSON, _ := json.Marshal(valuesMap)

	m3uChannel := M3UChannelStructXEPG{
		FileM3UID:       "test_m3u_id_1",
		FileM3UName:     "TestM3U",
		FileM3UPath:     "/path/to/m3u",
		Values:          string(valuesJSON), // Store as JSON string
		GroupTitle:      "Test Group",
		Name:            "Test Channel 1",
		TvgID:           "tvgId1",
		TvgLogo:         "logo1.png",
		TvgName:         "Tvg Name 1",
		TvgShift:        "1",
		URL:             "http://stream.url/1",
		UUIDKey:         "channel-id",
		UUIDValue:       "2005", // Changed from "uniqueId1" to a numeric string
		PreserveMapping: "true",
	}

	// Test Case 1: PreserveMapping = "true"
	Data.XEPG.Channels = make(map[string]any)
	processNewXEPGChannel(m3uChannel, &allChannelNumbers)

	if len(Data.XEPG.Channels) != 1 {
		t.Fatalf("Expected 1 channel in Data.XEPG.Channels, got %d", len(Data.XEPG.Channels))
	}

	var newXEPGID string
	for k := range Data.XEPG.Channels {
		newXEPGID = k
	}

	// The first generated ID should be "x-ID.0" if Data.XEPG.Channels was empty
	if newXEPGID != "x-ID.0" {
		t.Errorf("Expected XEPG ID to be 'x-ID.0', got '%s'", newXEPGID)
	}

	newChannelData := Data.XEPG.Channels[newXEPGID]
	newChannelBytes, _ := json.Marshal(newChannelData)
	var newChannel XEPGChannelStruct
	json.Unmarshal(newChannelBytes, &newChannel)

	// With UUIDValue = "2005" and PreserveMapping = true, XChannelID should be "2005"
	if newChannel.XChannelID != "2005" {
		t.Errorf("Expected XChannelID to be '2005', got '%s'", newChannel.XChannelID)
	}
	expectedChannelNumFloat, _ := strconv.ParseFloat("2005", 64)
	if !lo.Contains(allChannelNumbers, expectedChannelNumFloat) {
		t.Errorf("Expected %.1f to be added to allChannelNumbers. Got: %v", expectedChannelNumFloat, allChannelNumbers)
	}

	if newChannel.Name != m3uChannel.Name {
		t.Errorf("Expected Name to be '%s', got '%s'", m3uChannel.Name, newChannel.Name)
	}
	if newChannel.XName != m3uChannel.Name {
		t.Errorf("Expected XName to be '%s', got '%s'", m3uChannel.Name, newChannel.XName)
	}
	if newChannel.TvgShift != m3uChannel.TvgShift {
		t.Errorf("Expected TvgShift to be '%s', got '%s'", m3uChannel.TvgShift, newChannel.TvgShift)
	}
	if newChannel.XTimeshift != m3uChannel.TvgShift {
		t.Errorf("Expected XTimeshift to be '%s', got '%s'", m3uChannel.TvgShift, newChannel.XTimeshift)
	}
	if newChannel.UUIDValue != "2005" { // Check against the corrected UUIDValue
		t.Errorf("Expected UUIDValue to be '2005', got '%s'", newChannel.UUIDValue)
	}

	// Test Case 2: PreserveMapping = "false", StartingChannel used
	m3uChannel2 := M3UChannelStructXEPG{
		FileM3UID:       "test_m3u_id_2",
		Name:            "Test Channel 2",
		StartingChannel: "2500",
		PreserveMapping: "false",
		TvgShift:        "",
	}
	Data.XEPG.Channels = make(map[string]any)
	allChannelNumbers = []float64{}
	processNewXEPGChannel(m3uChannel2, &allChannelNumbers)

	var newXEPGID2 string
	i := 0
	for k := range Data.XEPG.Channels {
		newXEPGID2 = k
		i++
		if i > 1 {
			t.Fatal("More than one channel created for test case 2")
		}
	}

	if newXEPGID2 != "x-ID.0" {
		t.Errorf("Expected XEPG ID to be 'x-ID.0', got '%s'", newXEPGID2)
	}

	newChannelData2 := Data.XEPG.Channels[newXEPGID2]
	newChannelBytes2, _ := json.Marshal(newChannelData2)
	var newChannel2 XEPGChannelStruct
	json.Unmarshal(newChannelBytes2, &newChannel2)

	if newChannel2.XChannelID != "2500" {
		t.Errorf("Expected XChannelID to be '2500', got '%s'", newChannel2.XChannelID)
	}
	if !lo.Contains(allChannelNumbers, 2500.0) {
		t.Errorf("Expected 2500 to be added to allChannelNumbers. Got: %v", allChannelNumbers)
	}
	if newChannel2.TvgShift != "0" {
		t.Errorf("Expected TvgShift to be '0', got '%s'", newChannel2.TvgShift)
	}
	if newChannel2.XTimeshift != "0" {
		t.Errorf("Expected XTimeshift to be '0', got '%s'", newChannel2.XTimeshift)
	}

	// Test Case 3: No StartingChannel, PreserveMapping = "false", Settings.MappingFirstChannel should be used
	m3uChannel3 := M3UChannelStructXEPG{
		FileM3UID:       "test_m3u_id_3",
		Name:            "Test Channel 3",
		PreserveMapping: "false",
	}
	Data.XEPG.Channels = make(map[string]any)
	allChannelNumbers = []float64{}
	Settings.MappingFirstChannel = 3000
	processNewXEPGChannel(m3uChannel3, &allChannelNumbers)

	var newXEPGID3 string
	i = 0
	for k := range Data.XEPG.Channels {
		newXEPGID3 = k
		i++
		if i > 1 {
			t.Fatal("More than one channel created for test case 3")
		}
	}
	newChannelData3 := Data.XEPG.Channels[newXEPGID3]
	newChannelBytes3, _ := json.Marshal(newChannelData3)
	var newChannel3 XEPGChannelStruct
	json.Unmarshal(newChannelBytes3, &newChannel3)

	if newChannel3.XChannelID != "3000" {
		t.Errorf("Expected XChannelID to be '3000', got '%s'", newChannel3.XChannelID)
	}
	if !lo.Contains(allChannelNumbers, 3000.0) {
		t.Errorf("Expected 3000 to be added to allChannelNumbers. Got: %v", allChannelNumbers)
	}
}

// Ensure all necessary types from the original xepg.go that are used by the
// new functions or their tests are defined here or properly mocked.
// This includes M3UChannelStructXEPG and XEPGChannelStruct.
// The fields included should be the ones relevant to the tested functions.
// For example, XEPGChannelStruct needs fields like XEPG, Name, URL, XUpdateChannelName, etc.
// M3UChannelStructXEPG needs Name, URL, GroupTitle, TvgLogo, etc.
// These are now expected to be available from the src package itself.
// Ensure imgcache.Cache is properly typed (e.g., *imgcache.Cache) and can be nil if not used.
// XMLTV and StreamInfo should be available from the src package.
