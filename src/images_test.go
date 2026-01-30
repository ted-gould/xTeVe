package src

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestUploadLogoPathTraversal attempts to verify if uploadLogo allows writing files outside
// of System.Folder.ImagesUpload.
func TestUploadLogoPathTraversal(t *testing.T) {
	// Setup temporary directory structure for testing
	tempDir := t.TempDir()
	imagesUploadDir := filepath.Join(tempDir, "images_upload")
	if err := os.Mkdir(imagesUploadDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Mock System.Folder.ImagesUpload
	// Ensure it ends with separator as expected by the code usage in uploadLogo:
	// var file = fmt.Sprintf("%s%s", System.Folder.ImagesUpload, filename)
	System.Folder.ImagesUpload = imagesUploadDir + string(os.PathSeparator)

	// Also need to set System.ServerProtocol.XML and System.Domain for the return value construction
	System.ServerProtocol.XML = "http"
	System.Domain = "localhost:34400"

	// Define the target file outside the upload directory
	targetFilename := "traversal_test.png" // Changed to .png to satisfy extension check
	targetPath := filepath.Join(tempDir, targetFilename)

	// Construct a filename that uses path traversal to reach targetPath
	// We want to go up one level from images_upload to tempDir
	traversalFilename := fmt.Sprintf("..%c%s", os.PathSeparator, targetFilename)

	// Content to write (base64 encoded "test content")
	// "test content" in base64 is "dGVzdCBjb250ZW50"
	input := "data:image/png;base64,dGVzdCBjb250ZW50"

	// Call uploadLogo
	_, err := uploadLogo(input, traversalFilename, System.Domain)
	if err != nil {
		t.Fatalf("uploadLogo failed unexpectedly: %v", err)
	}

	// Verify if the file was created at targetPath (vulnerability check)
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Errorf("Security check failed: File was created at %s via path traversal.", targetPath)
	}

	// Verify it was created in the correct place (sanitized)
	// After fix, traversalFilename should be sanitized to just targetFilename
	sanitizedFilename := filepath.Base(targetFilename)
	expectedPath := filepath.Join(imagesUploadDir, sanitizedFilename)
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("File was not created at expected sanitized path %s", expectedPath)
	}
}
