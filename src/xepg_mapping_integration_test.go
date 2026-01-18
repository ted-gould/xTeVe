package src

import (
	"os"
	"slices"
	"testing"
)

// TestCreateXEPGMappingIntegration validates that createXEPGMapping correctly removes invalid XMLTV files
// from Data.XMLTV.Files using the real file system and the actual function logic.
func TestCreateXEPGMappingIntegration(t *testing.T) {
	// 1. Setup temporary test directory for data
	tmpDir := t.TempDir()

	// Preserve original state
	originalDataFolder := System.Folder.Data
	originalSettingsXMLTV := Settings.Files.XMLTV
	originalDataXMLTVFiles := Data.XMLTV.Files
	originalDataXMLTVMapping := Data.XMLTV.Mapping
	originalCacheXMLTV := Data.Cache.XMLTV

	defer func() {
		// Restore state
		System.Folder.Data = originalDataFolder
		Settings.Files.XMLTV = originalSettingsXMLTV
		Data.XMLTV.Files = originalDataXMLTVFiles
		Data.XMLTV.Mapping = originalDataXMLTVMapping
		Data.Cache.XMLTV = originalCacheXMLTV
	}()

	// Mock System.Folder.Data to point to temp dir.
	// Ensure trailing slash as usually expected by the app
	System.Folder.Data = tmpDir + string(os.PathSeparator)

	// 2. Setup Settings.Files.XMLTV with mock provider files
	Settings.Files.XMLTV = make(map[string]any)

	// "valid" provider -> valid.xml
	Settings.Files.XMLTV["valid"] = map[string]any{
		"name": "Valid Provider",
		"file.source": "http://example.com/valid.xml",
	}

	// "invalid" provider -> invalid.xml
	Settings.Files.XMLTV["invalid"] = map[string]any{
		"name": "Invalid Provider",
		"file.source": "http://example.com/invalid.xml",
	}

	// 3. Create actual files in the temp directory
	// Note: getLocalProviderFiles constructs paths as System.Folder.Data + dataID + ".xml"

	// Create valid.xml
	validXMLContent := `<?xml version="1.0" encoding="UTF-8"?>
<tv generator-info-name="xTeVe">
  <channel id="test.channel">
    <display-name>Test Channel</display-name>
  </channel>
</tv>`
	err := os.WriteFile(tmpDir + string(os.PathSeparator) + "valid.xml", []byte(validXMLContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create valid.xml: %v", err)
	}

	// Create invalid.xml (corrupt content)
	invalidXMLContent := `This is not XML content`
	err = os.WriteFile(tmpDir + string(os.PathSeparator) + "invalid.xml", []byte(invalidXMLContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create invalid.xml: %v", err)
	}

	// Clear cache to ensure files are re-read
	Data.Cache.XMLTV = make(map[string]XMLTV)

	// 4. Run the function under test
	createXEPGMapping()

	// 5. Assertions
	// We expect Data.XMLTV.Files to contain only the valid file path.

	expectedValidPath := System.Folder.Data + "valid.xml"
	expectedInvalidPath := System.Folder.Data + "invalid.xml"

	if !slices.Contains(Data.XMLTV.Files, expectedValidPath) {
		t.Errorf("Expected Data.XMLTV.Files to contain valid file %s, but it did not. Got: %v", expectedValidPath, Data.XMLTV.Files)
	}

	if slices.Contains(Data.XMLTV.Files, expectedInvalidPath) {
		t.Errorf("Expected Data.XMLTV.Files to NOT contain invalid file %s, but it did. Got: %v", expectedInvalidPath, Data.XMLTV.Files)
	}

	// Check if the mapping was created for the valid file
	if _, ok := Data.XMLTV.Mapping["valid.xml"]; !ok {
		t.Errorf("Expected Data.XMLTV.Mapping to contain key 'valid.xml', but it did not.")
	}
}
