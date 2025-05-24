package src

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Mock structures and setup for mapping tests
var testMappingSystem = SystemStruct{ // src.SystemStruct
	Folder: struct { // Anonymous struct matching SystemStruct.Folder
		Backup       string
		Cache        string
		Certificates string
		Config       string
		Data         string
		ImagesCache  string
		ImagesUpload string
		Temp         string
	}{
		Data: "./testdata/",
	},
	File: struct { // Anonymous struct matching SystemStruct.File
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
		XEPG: "./testdata/xepg_mapping_test.json", 
	},
}

var testMappingSettings = SettingsStruct{ // src.SettingsStruct
	DefaultMissingEPG:   "default_dummy", // e.g., "60_Minutes"
	EnableMappedChannels: true,
}

// Helper to setup global state for mapping tests
func setupMappingTestGlobals() func() {
	originalSystem := System
	originalSettings := Settings
	originalData := Data

	System = testMappingSystem
	Settings = testMappingSettings
	
	// Fresh Data for each test
	Data = DataStruct{
		XMLTV: struct {
			Files   []string
			Mapping map[string]any
		}{
			Mapping: make(map[string]any),
		},
		XEPG: struct {
			Channels  map[string]any
			XEPGCount int64
		}{
			Channels: make(map[string]any),
		},
		// Initialize other Data fields if necessary for the functions under test
	}

	// Pre-populate Data.XMLTV.Mapping for tests
	// Sample XMLTV file "test_provider.xml"
	Data.XMLTV.Mapping["test_provider.xml"] = map[string]any{
		"channel1.tvg.id": map[string]any{
			"id":   "channel1.tvg.id",
			"display-names": []DisplayName{{Value: "Test Channel 1 TVG-ID"}},
			"icon": "icon1.png",
		},
		"channel2.name.match": map[string]any{
			"id":   "channel2.name.match",
			"display-names": []DisplayName{{Value: "Test Channel NameMatch"}},
			"icon": "icon2.png",
		},
	}
	// Sample Dummy EPG
	Data.XMLTV.Mapping["xTeVe Dummy"] = map[string]any{
		"default_dummy": map[string]any{
			"id":   "default_dummy",
			"display-names": []DisplayName{{Value: "Default Dummy EPG"}},
			"icon": "",
		},
		"60_Minutes": map[string]any{
			"id": "60_Minutes",
			"display-names": []DisplayName{{Value: "60 Minutes"}},
			"icon": "",
		},
	}

	if err := os.MkdirAll(System.Folder.Data, os.ModePerm); err != nil {
		// This is a fatal error for test setup. Panic is appropriate here
		// as t is not available to call t.Fatalf.
		panic(fmt.Sprintf("Failed to create test data directory in setupMappingTestGlobals: %v", err))
	}
	os.Remove(System.File.XEPG) // Error benign if file doesn't exist

	return func() {
		System = originalSystem
		Settings = originalSettings
		Data = originalData
		os.Remove(System.File.XEPG)
	}
}

func TestPerformAutomaticChannelMapping(t *testing.T) {
	teardown := setupMappingTestGlobals()
	defer teardown()

	xepgID := "x-ID.test"

	tests := []struct {
		name                string
		initialChannel      XEPGChannelStruct
		settingsDefaultEPG  string
		expectedChannel     XEPGChannelStruct
		expectedMappingMade bool
	}{
		{
			name: "inactive no mapping, tvg-id match",
			initialChannel: XEPGChannelStruct{
				XActive:    false,
				Name:       "Channel With TVG-ID",
				TvgID:      "channel1.tvg.id", // Matches Data.XMLTV.Mapping
				XmltvFile:  "",
				XMapping:   "",
				TvgLogo:    "original_logo.png",
			},
			settingsDefaultEPG: "default_dummy",
			expectedChannel: XEPGChannelStruct{
				XActive:    false,
				Name:       "Channel With TVG-ID",
				TvgID:      "channel1.tvg.id",
				XmltvFile:  "test_provider.xml",
				XMapping:   "channel1.tvg.id",
				TvgLogo:    "icon1.png", // Updated from XMLTV
			},
			expectedMappingMade: true,
		},
		{
			name: "inactive no mapping, name match",
			initialChannel: XEPGChannelStruct{
				XActive:    false,
				Name:       "Test Channel NameMatch", // Name will be used for matching
				TvgID:      "non.existent.tvg.id",
				XmltvFile:  "-", // Indicates no mapping
				XMapping:   "-", // Indicates no mapping
				TvgLogo:    "original_logo.png",
			},
			settingsDefaultEPG: "default_dummy",
			expectedChannel: XEPGChannelStruct{
				XActive:    false,
				Name:       "Test Channel NameMatch",
				TvgID:      "non.existent.tvg.id",
				XmltvFile:  "test_provider.xml",
				XMapping:   "channel2.name.match",
				TvgLogo:    "icon2.png", // Updated from XMLTV
			},
			expectedMappingMade: true,
		},
		{
			name: "inactive no mapping, no match, default EPG applied",
			initialChannel: XEPGChannelStruct{
				XActive:    false,
				Name:       "No Match Channel",
				TvgID:      "another.non.existent.id",
				XmltvFile:  "",
				XMapping:   "",
				TvgLogo:    "original_logo.png",
			},
			settingsDefaultEPG: "default_dummy", // Setting this default
			expectedChannel: XEPGChannelStruct{
				XActive:    false,
				Name:       "No Match Channel",
				TvgID:      "another.non.existent.id",
				XmltvFile:  "xTeVe Dummy",        // Default applied
				XMapping:   "default_dummy",      // Default applied
				TvgLogo:    "original_logo.png", // Not updated if dummy EPG has no icon or icon is empty
			},
			expectedMappingMade: true,
		},
		{
			name: "inactive no mapping, no match, no default EPG (-)",
			initialChannel: XEPGChannelStruct{
				XActive:    false,
				Name:       "No Match Channel No Default",
				TvgID:      "yet.another.id",
				XmltvFile:  "",
				XMapping:   "",
			},
			settingsDefaultEPG: "-", // Setting explicitly no default
			expectedChannel: XEPGChannelStruct{
				XActive:    false,
				Name:       "No Match Channel No Default",
				TvgID:      "yet.another.id",
				XmltvFile:  "-", // Should remain "-"
				XMapping:   "-", // Should remain "-"
			},
			expectedMappingMade: false, // No mapping made, not even default
		},
		{
			name: "active channel, should not attempt mapping",
			initialChannel: XEPGChannelStruct{
				XActive:    true, // Active
				Name:       "Already Active",
				TvgID:      "active.tvg.id",
				XmltvFile:  "some_file.xml",
				XMapping:   "some_mapping",
			},
			settingsDefaultEPG: "default_dummy",
			expectedChannel: XEPGChannelStruct{ // Expected to be unchanged
				XActive:    true,
				Name:       "Already Active",
				TvgID:      "active.tvg.id",
				XmltvFile:  "some_file.xml",
				XMapping:   "some_mapping",
			},
			expectedMappingMade: false,
		},
		{
			name: "inactive but already has XmltvFile, should not attempt mapping",
			initialChannel: XEPGChannelStruct{
				XActive:    false,
				Name:       "Inactive With File",
				TvgID:      "inactive.file.id",
				XmltvFile:  "pre_existing_file.xml", // Already has a file
				XMapping:   "", // Mapping is empty, but file is not <= 1 length
			},
			settingsDefaultEPG: "default_dummy",
			expectedChannel: XEPGChannelStruct{ // Expected to be unchanged by this function's core logic
				XActive:    false,
				Name:       "Inactive With File",
				TvgID:      "inactive.file.id",
				XmltvFile:  "pre_existing_file.xml",
				XMapping:   "",
			},
			expectedMappingMade: false, // No new mapping attempted
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalDefaultEPG := Settings.DefaultMissingEPG
			Settings.DefaultMissingEPG = tt.settingsDefaultEPG
			defer func() { Settings.DefaultMissingEPG = originalDefaultEPG }()

			// The function performAutomaticChannelMapping modifies the xepgChannel struct passed to it
			// and Data.XEPG.Channels map. For testing, we only care about the returned struct.
			// The original function also wrote to Data.XEPG.Channels[xepgID], but this responsibility
			// is now with the caller (the main mapping() function).
			// So, we don't need to check Data.XEPG.Channels here directly for this unit test.

			resultChannel, mappingMade := performAutomaticChannelMapping(tt.initialChannel, xepgID)

			if mappingMade != tt.expectedMappingMade {
				t.Errorf("performAutomaticChannelMapping mappingMade: got %v, want %v", mappingMade, tt.expectedMappingMade)
			}
			
			// Normalizing XEPGChannelStruct fields that might be nil slices if empty, for comparison.
			// For example, if a field is `[]DisplayName` and it's empty, it could be nil or empty slice.
			// cmp.Diff should handle this, but good to be aware.

			if diff := cmp.Diff(tt.expectedChannel, resultChannel); diff != "" {
				t.Errorf("performAutomaticChannelMapping channel mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestVerifyExistingChannelMappings(t *testing.T) {
	teardown := setupMappingTestGlobals()
	defer teardown()

	tests := []struct {
		name            string
		initialChannel  XEPGChannelStruct
		xmltvMapping    map[string]any // To set Data.XMLTV.Mapping for the test
		expectedChannel XEPGChannelStruct
	}{
		{
			name: "active, valid mapping, logo update",
			initialChannel: XEPGChannelStruct{
				XActive:            true,
				Name:               "Valid Channel",
				XmltvFile:          "test_provider.xml",
				XMapping:           "channel1.tvg.id",
				TvgLogo:            "original_logo.png",
				XUpdateChannelIcon: true,
			},
			xmltvMapping: Data.XMLTV.Mapping, // Use pre-populated
			expectedChannel: XEPGChannelStruct{
				XActive:            true,
				Name:               "Valid Channel",
				XmltvFile:          "test_provider.xml",
				XMapping:           "channel1.tvg.id",
				TvgLogo:            "icon1.png", // Updated from XMLTV mapping
				XUpdateChannelIcon: true,
			},
		},
		{
			name: "active, XUpdateChannelIcon false, logo not updated",
			initialChannel: XEPGChannelStruct{
				XActive:            true,
				Name:               "Valid Channel NoLogoUpdate",
				XmltvFile:          "test_provider.xml",
				XMapping:           "channel1.tvg.id",
				TvgLogo:            "original_logo.png",
				XUpdateChannelIcon: false, // Icon update disabled
			},
			xmltvMapping: Data.XMLTV.Mapping,
			expectedChannel: XEPGChannelStruct{
				XActive:            true,
				Name:               "Valid Channel NoLogoUpdate",
				XmltvFile:          "test_provider.xml",
				XMapping:           "channel1.tvg.id",
				TvgLogo:            "original_logo.png", // Should remain original
				XUpdateChannelIcon: false,
			},
		},
		{
			name: "active, xmltv file missing",
			initialChannel: XEPGChannelStruct{
				XActive:   true,
				Name:      "File Missing Channel",
				XmltvFile: "non_existent_provider.xml", // This file is not in Data.XMLTV.Mapping
				XMapping:  "any.mapping",
			},
			xmltvMapping: Data.XMLTV.Mapping,
			expectedChannel: XEPGChannelStruct{
				XActive:   false, // Should be deactivated
				Name:      "File Missing Channel",
				XmltvFile: "-",     // Should be set to -
				XMapping:  "-",     // Should be set to -
			},
		},
		{
			name: "active, xmltv channel (mapping) missing",
			initialChannel: XEPGChannelStruct{
				XActive:   true,
				Name:      "Mapping Missing Channel",
				XmltvFile: "test_provider.xml",
				XMapping:  "non_existent.mapping.id", // This ID is not in "test_provider.xml"
			},
			xmltvMapping: Data.XMLTV.Mapping,
			expectedChannel: XEPGChannelStruct{
				XActive:   false, // Should be deactivated
				Name:      "Mapping Missing Channel",
				XmltvFile: "-", // Should be set to - (as file is valid, but mapping causes deactivation)
				XMapping:  "-", // Should be set to -
			},
		},
		{
			name: "active, dummy mapping, remains active",
			initialChannel: XEPGChannelStruct{
				XActive:   true,
				Name:      "Dummy Channel",
				XmltvFile: "xTeVe Dummy", // Dummy file
				XMapping:  "60_Minutes",  // Valid dummy mapping
			},
			xmltvMapping: Data.XMLTV.Mapping,
			expectedChannel: XEPGChannelStruct{ // Expected to remain active, no logo change if dummy has none
				XActive:   true,
				Name:      "Dummy Channel",
				XmltvFile: "xTeVe Dummy",
				XMapping:  "60_Minutes",
			},
		},
		{
			name: "not active, should not be processed",
			initialChannel: XEPGChannelStruct{
				XActive:   false, // Not active
				Name:      "Inactive Channel",
				XmltvFile: "test_provider.xml",
				XMapping:  "channel1.tvg.id",
			},
			xmltvMapping: Data.XMLTV.Mapping,
			expectedChannel: XEPGChannelStruct{ // Should be returned unchanged
				XActive:   false,
				Name:      "Inactive Channel",
				XmltvFile: "test_provider.xml",
				XMapping:  "channel1.tvg.id",
			},
		},
		{
			name: "active, but XmltvFile becomes empty, should deactivate and set to '-'",
			initialChannel: XEPGChannelStruct{
				XActive:   true,
				Name:      "Active Becomes Empty File",
				XmltvFile: "", // Invalid state for an active channel post-checks
				XMapping:  "channel1.tvg.id",
			},
			xmltvMapping: Data.XMLTV.Mapping,
			expectedChannel: XEPGChannelStruct{
				XActive:   false,
				Name:      "Active Becomes Empty File",
				XmltvFile: "-",
				XMapping:  "-",
			},
		},
		{
			name: "active, but XMapping becomes empty, should deactivate and set to '-'",
			initialChannel: XEPGChannelStruct{
				XActive:   true,
				Name:      "Active Becomes Empty Mapping",
				XmltvFile: "test_provider.xml",
				XMapping:  "", // Invalid state for an active channel post-checks
			},
			xmltvMapping: Data.XMLTV.Mapping,
			expectedChannel: XEPGChannelStruct{
				XActive:   false,
				Name:      "Active Becomes Empty Mapping",
				XmltvFile: "-",
				XMapping:  "-",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set specific XMLTV mapping for this test if provided, else use default from setup
			if tt.xmltvMapping != nil {
				originalMapping := Data.XMLTV.Mapping
				Data.XMLTV.Mapping = tt.xmltvMapping
				defer func() { Data.XMLTV.Mapping = originalMapping }()
			}

			resultChannel := verifyExistingChannelMappings(tt.initialChannel)

			if diff := cmp.Diff(tt.expectedChannel, resultChannel); diff != "" {
				t.Errorf("verifyExistingChannelMappings channel mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Minimal stubs for types used if not directly available or for clarity in test setup
// These should ideally come from the main package if tests are in the same package.
// type XEPGChannelStruct already defined in src/struct-system.go
// type DisplayName already defined in src/struct-xml.go

// getProviderParameter is defined in src/data.go and should be accessible.
// showWarning and ShowError are defined in src/toolchain.go and should be accessible.
// No need for mocks of these here.

// Dummy saveMapToJSONFile is not needed as unit tests focus on returned values,
// not side effects like file saving for these specific functions.
