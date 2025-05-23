package src

import (
	"fmt" // Re-add for panic message
	"os"
	"path"
	"strings"
	"testing"
	"xteve/src/internal/imgcache"

	"github.com/google/go-cmp/cmp"
)

// --- Test Setup ---
var testXMLTVSystem = SystemStruct{
	Folder: struct {
		Backup       string
		Cache        string
		Certificates string
		Config       string
		Data         string
		ImagesCache  string
		ImagesUpload string
		Temp         string
	}{
		Data:        "./testdata/",
		ImagesCache: "./testdata/cache/images/", // Used by imgcache.New
	},
	File: struct {
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
		XML:  "./testdata/test_xepg.xml",
		XEPG: "./testdata/test_xepg.json",
	},
	ServerProtocol: struct {
		API string
		DVR string
		M3U string
		WEB string
		XML string
	}{
		WEB: "http", API: "http", DVR: "http", M3U: "http", XML: "http", // Provide all fields
	},
	Domain:  "localhost:34400", // Used by imgcache.New for default cacheURL
	Name:    "xTeVe Test",
	Version: "test.v",
	Build:   "test.b",
}

var testXMLTVSettings = SettingsStruct{
	CacheImages:              false, // Keep false to simplify testing GetURL if imgcache.New not fully mocked
	XepgReplaceMissingImages: false, // Affects createDummyProgram behavior
	// Other settings as needed by getProgramData or its sub-functions
}

func setupXMLTVTestGlobals() func() {
	originalSystem := System
	originalSettings := Settings
	originalData := Data

	System = testXMLTVSystem
	Settings = testXMLTVSettings

	// Initialize Data.Cache.Images with a real imgcache.Cache instance, but with caching disabled.
	// When caching is false, GetURL returns the src as-is, simplifying tests.
	// The cacheURL for imgcache.New is formed using System.ServerProtocol.WEB and System.Domain
	cachePath := System.Folder.ImagesCache
	os.MkdirAll(cachePath, os.ModePerm) // Ensure cache path exists for New()

	// The cacheURL for imgcache.New is not critical if caching is false for GetURL behavior.
	// However, to fully initialize, use values from testXMLTVSystem.
	imgCacheInstance, err := imgcache.New(cachePath,
		testXMLTVSystem.ServerProtocol.WEB+"://"+testXMLTVSystem.Domain+"/images/",
		false) // IMPORTANT: caching = false
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize imgcache for tests: %v", err))
	}

	Data = DataStruct{
		XEPG: struct {
			Channels  map[string]any
			XEPGCount int64
		}{
			Channels: make(map[string]any),
		},
		Cache: struct {
			Images        *imgcache.Cache
			ImagesCache   []string
			ImagesFiles   []string
			ImagesURLS    []string
			PMS           map[string]string
			StreamingURLS map[string]StreamInfo
			XMLTV         map[string]XMLTV
			Streams       struct {
				Active []string
			}
		}{
			XMLTV:  make(map[string]XMLTV),
			Images: imgCacheInstance, // Use the real imgcache instance with caching=false
		},
		XMLTV: struct {
			Files   []string
			Mapping map[string]any
		}{
			Mapping: make(map[string]any),
		},
	}

	os.MkdirAll(System.Folder.Data, os.ModePerm)
	// Create a dummy provider XMLTV file for getLocalXMLTV to read
	dummyXMLContent := `
	<tv>
		<channel id="dummy.ch1">
			<display-name>Dummy Channel 1</display-name>
		</channel>
		<programme channel="dummy.ch1" start="20240101000000 +0000" stop="20240101010000 +0000">
			<title>Dummy Program 1</title>
			<desc>Description for Dummy Program 1</desc>
		</programme>
	</tv>
	`
	// For getLocalXMLTV, the filename is Data.Folder.data + xepgChannel.XmltvFile
	// So if xepgChannel.XmltvFile = "provider.xml", path is "./testdata/provider.xml"
	os.WriteFile(path.Join(System.Folder.Data, "provider.xml"), []byte(dummyXMLContent), 0644)

	return func() {
		System = originalSystem
		Settings = originalSettings
		Data = originalData
		os.Remove(path.Join(System.Folder.Data, "provider.xml"))
		os.Remove(System.File.XML) // Clean up generated XML file
	}
}

// --- Tests for createChannelElements ---
func TestCreateChannelElements(t *testing.T) {
	teardown := setupXMLTVTestGlobals()
	defer teardown()

	// Get the imgcache instance configured in Data for tests
	// This instance has caching disabled, so GetURL(src) will return src.
	imgCacheForTests := Data.Cache.Images

	nilImgCacheForTests := (*imgcache.Cache)(nil) // A nil *imgcache.Cache

	// The case for imgCacheWithNilFunc (where Image.GetURL is nil) is removed
	// because imgcache.New() always sets Image.GetURL to a valid function.
	// It's not possible to have a nil Image.GetURL with a normally constructed *imgcache.Cache.

	tests := []struct {
		name        string
		imgc        *imgcache.Cache
		xepgChannel XEPGChannelStruct
		expected    *Channel
	}{
		{
			name: "basic channel",
			imgc: imgCacheForTests,
			xepgChannel: XEPGChannelStruct{
				XChannelID: "101.1",
				XName:      "Test Channel One",
				TvgLogo:    "logo.png", // Relative path
			},
			expected: &Channel{
				ID:           "101.1",
				DisplayNames: []DisplayName{{Value: "Test Channel One"}},
				Icon:         Icon{Src: "logo.png"}, // Expect as-is due to caching=false
			},
		},
		{
			name: "channel with empty logo",
			imgc: imgCacheForTests,
			xepgChannel: XEPGChannelStruct{
				XChannelID: "102",
				XName:      "No Logo Channel",
				TvgLogo:    "",
			},
			expected: &Channel{
				ID:           "102",
				DisplayNames: []DisplayName{{Value: "No Logo Channel"}},
				Icon:         Icon{Src: ""},
			},
		},
		{
			name: "channel with full URL logo",
			imgc: imgCacheForTests,
			xepgChannel: XEPGChannelStruct{
				XChannelID: "103",
				XName:      "Full URL Logo Channel",
				TvgLogo:    "https://some.cdn.com/image.jpg", // Absolute URL
			},
			expected: &Channel{
				ID:           "103",
				DisplayNames: []DisplayName{{Value: "Full URL Logo Channel"}},
				Icon:         Icon{Src: "https://some.cdn.com/image.jpg"}, // Expect as-is
			},
		},
		{
			name: "nil imgcache provided",
			imgc: nilImgCacheForTests, // Pass a nil *imgcache.Cache
			xepgChannel: XEPGChannelStruct{
				XChannelID: "104",
				XName:      "Nil Cache Channel",
				TvgLogo:    "logo_nil_cache.png",
			},
			expected: &Channel{
				ID:           "104",
				DisplayNames: []DisplayName{{Value: "Nil Cache Channel"}},
				Icon:         Icon{Src: "logo_nil_cache.png"}, // Fallback to TvgLogo directly
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := createChannelElements(tt.xepgChannel, tt.imgc)
			if diff := cmp.Diff(tt.expected, got); diff != "" {
				t.Errorf("createChannelElements() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// --- Tests for createProgramElements ---
func TestCreateProgramElements(t *testing.T) {
	teardown := setupXMLTVTestGlobals()
	defer teardown()

	// For getProgramData (called by createProgramElements) to work,
	// Data.Cache.XMLTV needs to be populated by getLocalXMLTV,
	// or getLocalXMLTV needs to be callable (reads from file system).
	// The setupXMLTVTestGlobals creates a dummy "provider.xml".

	tests := []struct {
		name          string
		xepgChannel   XEPGChannelStruct
		wantErr       bool
		expectedCount int    // Number of programs expected
		expectedTitle string // Title of the first program if count > 0
	}{
		{
			name: "programs from dummy provider.xml",
			xepgChannel: XEPGChannelStruct{
				XChannelID: "x.1", // This will be the program's channel ID in output
				XName:      "My Test Channel",
				XmltvFile:  "provider.xml", // Matches the dummy file created in setup
				XMapping:   "dummy.ch1",    // Channel ID within provider.xml
				XTimeshift: "0",
			},
			wantErr:       false,
			expectedCount: 1,
			expectedTitle: "Dummy Program 1",
		},
		{
			name: "programs from xTeVe Dummy EPG",
			xepgChannel: XEPGChannelStruct{
				XChannelID:   "x.2",
				XName:        "My Dummy EPG Channel",
				XmltvFile:    "xTeVe Dummy", // Use dummy EPG
				XMapping:     "60_Minutes",  // A valid dummy mapping
				XTimeshift:   "0",
				XDescription: "Custom dummy description",
			},
			wantErr:       false,
			expectedCount: 4 * (1440 / 60), // 4 days * (programs per day)
			// Title will be like "My Dummy EPG Channel (Mo. 00:00 - 01:00)"
			// Just check if the channel name is in the title
			expectedTitle: "My Dummy EPG Channel",
		},
		{
			name: "error from getLocalXMLTV - file not found",
			xepgChannel: XEPGChannelStruct{
				XChannelID: "x.3",
				XName:      "Channel With Bad File",
				XmltvFile:  "non_existent_provider.xml", // This file does not exist
				XMapping:   "any.id",
			},
			wantErr:       true,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// If testing dummy EPG, ensure Settings.XepgReplaceMissingImages is considered.
			// For this test, it's false by default in testXMLTVSettings.
			programs, err := createProgramElements(tt.xepgChannel)

			if (err != nil) != tt.wantErr {
				t.Errorf("createProgramElements() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return // Error was expected, no further checks.
			}

			if len(programs) != tt.expectedCount {
				t.Errorf("createProgramElements() expected %d programs, got %d", tt.expectedCount, len(programs))
			}

			if tt.expectedCount > 0 && len(programs) > 0 {
				if !strings.Contains(programs[0].Title[0].Value, tt.expectedTitle) {
					t.Errorf("createProgramElements() first program title: got '%s', expected to contain '%s'", programs[0].Title[0].Value, tt.expectedTitle)
				}
				// Check that program channel ID matches XChannelID
				if programs[0].Channel != tt.xepgChannel.XChannelID {
					t.Errorf("createProgramElements() program channel ID: got '%s', want '%s'", programs[0].Channel, tt.xepgChannel.XChannelID)
				}
			}
		})
	}
}

// --- Minimal Stubs for compilation if not using actual types from src ---
// These should ideally be resolved from the package 'src' itself when tests are run.
// If `SystemFolder` or `SystemFile` are not actual exported types, use anonymous structs
// as done in xepg_mapping_test.go for SystemStruct.
// For this test file, SystemStruct, SettingsStruct, DataStruct, XEPGStruct, CacheStruct,
// XMLTVData, XEPGChannelStruct, Channel, Icon, DisplayName, Program are assumed to be
// defined and accessible from the 'src' package.

// Assuming SystemFolder and SystemFile are not specific exported types,
// the SystemStruct literal should use anonymous structs for Folder and File fields.
// This was corrected in the testMappingSystem declaration above.
// It's important that testXMLTVSystem also follows this.
// The definition of testXMLTVSystem is:
// var testXMLTVSystem = SystemStruct { Folder: SystemFolder {...}, File: SystemFile {...} }
// This needs to be:
// var testXMLTVSystem = SystemStruct { Folder: struct { ... } {...}, File: struct { ... } {...} }
// (Corrected SystemFolder/File usage in the actual test code above)
// type SystemFolder struct { Data string; ImagesCache string; } // Example, if it were a type
// type SystemFile struct { XML string; XEPG string; } // Example

// XMLTVData is the type for Data.XMLTV, it might be `struct { Files []string; Mapping map[string]any }`
// If it's an exported type, then `src.XMLTVData` should be used.
// If it's an anonymous struct field, then the literal must match.
// For Data.XMLTV, the actual field is `XMLTV struct { Files []string; Mapping map[string]any }`
// So the test setup for Data.XMLTV should be:
/*
Data = DataStruct{
	// ...
	XMLTV: struct { // This matches the anonymous struct type in DataStruct
		Files   []string
		Mapping map[string]any
	}{
		Mapping: make(map[string]any),
	},
	//...
}
*/
// This was corrected in the `setupXMLTVTestGlobals` for `Data.XMLTV`.
// The `XEPGStruct`, `CacheStruct`, `XMLTVData` used in `setupXMLTVTestGlobals`
// are assumed to be the actual types from the `src` package.
// Note: `imgcache.Cache` is `*imgcache.Cache` in `DataStruct`, so `&imgcache.Cache{}` is appropriate.
// `imgcache.CacheImage` is an interface. `mockMinimalCacheImage` implements it.
// `Data.Cache.Images.Image` should be assigned `&mockMinimalCacheImage{...}`.
// This is done in `setupXMLTVTestGlobals`.
// `SystemFolder` and `SystemFile` were placeholders in the comment, the actual struct uses anonymous ones.
// `testXMLTVSystem` definition was updated to use anonymous structs for Folder and File.
// `XMLTVData` was a placeholder for the type of `Data.XMLTV`. The actual anonymous struct is used in `setupXMLTVTestGlobals`.
