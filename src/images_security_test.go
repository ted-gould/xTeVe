package src

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUploadLogoFileExtensionSecurity verifies that uploadLogo rejects non-image file extensions.
func TestUploadLogoFileExtensionSecurity(t *testing.T) {
	// Setup temporary directory structure for testing
	tempDir := t.TempDir()
	imagesUploadDir := filepath.Join(tempDir, "images_upload")
	if err := os.Mkdir(imagesUploadDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Mock System.Folder.ImagesUpload
	originalImagesUpload := System.Folder.ImagesUpload
	System.Folder.ImagesUpload = imagesUploadDir + string(os.PathSeparator)
	defer func() { System.Folder.ImagesUpload = originalImagesUpload }()

	// Also need to set System.ServerProtocol.XML and System.Domain for the return value construction
	originalProtocol := System.ServerProtocol.XML
	originalDomain := System.Domain
	System.ServerProtocol.XML = "http"
	System.Domain = "localhost:34400"
	defer func() {
		System.ServerProtocol.XML = originalProtocol
		System.Domain = originalDomain
	}()

	// Content to write (base64 encoded "test content")
	input := "data:image/png;base64,dGVzdCBjb250ZW50"

	// Test cases
	tests := []struct {
		filename    string
		shouldError bool
	}{
		{"test.html", true},  // Should be rejected
		{"test.js", true},    // Should be rejected
		{"test.php", true},   // Should be rejected
		{"test.exe", true},   // Should be rejected
		{"test.sh", true},    // Should be rejected
		{"test.png", false},  // Should be accepted
		{"test.jpg", false},  // Should be accepted
		{"test.jpeg", false}, // Should be accepted
		{"test.gif", false},  // Should be accepted
		{"test.svg", false},  // Should be accepted
		{"test.ico", false},  // Should be accepted
	}

	for _, tc := range tests {
		t.Run(tc.filename, func(t *testing.T) {
			_, err := uploadLogo(input, tc.filename)
			if tc.shouldError && err == nil {
				t.Errorf("Security check failed: uploadLogo allowed forbidden extension for %s", tc.filename)
			}
			if !tc.shouldError && err != nil {
				t.Errorf("uploadLogo failed for valid extension %s: %v", tc.filename, err)
			}

			// If it shouldn't error, verify file exists
			if !tc.shouldError {
				expectedPath := filepath.Join(imagesUploadDir, tc.filename)
				if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
					t.Errorf("File was not created at expected path %s", expectedPath)
				}
			} else {
				// If it should error, verify file does NOT exist
				expectedPath := filepath.Join(imagesUploadDir, tc.filename)
				if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
					t.Errorf("File was created despite error expectation: %s", expectedPath)
				}
			}
		})
	}
}
