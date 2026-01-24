package src

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadHandler_StreamsAndDeletes(t *testing.T) {
	// Setup Temp Folder
	tempDir, err := os.MkdirTemp("", "xteve_test_download")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir) // Clean up the temp dir

	// Save original System.Folder.Temp
	originalTempFolder := System.Folder.Temp
	defer func() { System.Folder.Temp = originalTempFolder }()

	// Override System.Folder.Temp
	// Ensure trailing slash as the code concatenates directly
	System.Folder.Temp = tempDir + string(os.PathSeparator)

	// Create a dummy file
	filename := "test_backup.zip"
	content := "This is a test backup file content."
	fullPath := filepath.Join(tempDir, filename)
	err = os.WriteFile(fullPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create Request
	req := httptest.NewRequest("GET", "/download/"+filename, nil)
	w := httptest.NewRecorder()

	// Call Handler
	Download(w, req)

	// Verify Response
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %v", resp.StatusCode)
	}

	// Check Body
	if w.Body.String() != content {
		t.Errorf("Expected body %q, got %q", content, w.Body.String())
	}

	// Verify File Deletion
	_, err = os.Stat(fullPath)
	if !os.IsNotExist(err) {
		t.Errorf("File %s should be deleted after download, but error is: %v", fullPath, err)
	}
}
