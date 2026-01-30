package src

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUploadLogoStoredXSS attempts to verify if uploadLogo prevents uploading
// non-image files like HTML which could lead to Stored XSS.
func TestUploadLogoStoredXSS(t *testing.T) {
	// Setup temporary directory structure for testing
	tempDir := t.TempDir()
	imagesUploadDir := filepath.Join(tempDir, "images_upload")
	if err := os.Mkdir(imagesUploadDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Mock System.Folder.ImagesUpload
	originalImagesUpload := System.Folder.ImagesUpload
	originalXML := System.ServerProtocol.XML
	originalDomain := System.Domain

	defer func() {
		System.Folder.ImagesUpload = originalImagesUpload
		System.ServerProtocol.XML = originalXML
		System.Domain = originalDomain
	}()

	System.Folder.ImagesUpload = imagesUploadDir + string(os.PathSeparator)
	System.ServerProtocol.XML = "http"
	System.Domain = "localhost:34400"

	// malicious filename
	maliciousFilename := "hacker.html"

	// Content to write (base64 encoded "<h1>XSS</h1>")
	// "<h1>XSS</h1>" -> "PGgxPlhTUzwvaDE+"
	input := "data:text/html;base64,PGgxPlhTUzwvaDE+"

	// Call uploadLogo
	_, err := uploadLogo(input, maliciousFilename, System.Domain)

	// We EXPECT an error here if security is enforced.
	// If err is nil, it means the upload succeeded, which is a vulnerability.
	if err == nil {
		t.Errorf("Security check failed: Allowed uploading .html file which leads to Stored XSS.")
	} else {
		// If we got an error, check if it's the expected error (optional, but good practice)
		// For now, just having an error is enough to say we blocked it.
		t.Logf("Upload blocked as expected: %v", err)
	}

	// Also check if file exists on disk
	expectedPath := filepath.Join(imagesUploadDir, maliciousFilename)
	if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
		t.Errorf("Security check failed: File was written to disk at %s", expectedPath)
	}
}
