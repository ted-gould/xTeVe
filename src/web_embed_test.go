package src

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWebUIEmbed(t *testing.T) {
	// GIVEN
	// A list of all files in the "src/html" directory
	var expectedFiles []string
	err := filepath.Walk("html", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			expectedFiles = append(expectedFiles, path)
		}
		return nil
	})
	assert.NoError(t, err, "Failed to walk 'html' directory")

	// WHEN
	// We check if each file exists in the embedded FS
	for _, expectedFile := range expectedFiles {
		// THEN
		// The file should exist in the embedded FS
		_, err := webUI.ReadFile(expectedFile)
		assert.NoError(t, err, "File '%s' should be embedded", expectedFile)
	}
}

func TestWebHandler(t *testing.T) {
	// GIVEN
	// A list of all files in the "src/html" directory
	var testFiles []string
	err := filepath.Walk("html", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// Exclude the ".ts" file as it's not a web asset
			if !strings.HasSuffix(path, ".ts") {
				testFiles = append(testFiles, path)
			}
		}
		return nil
	})
	assert.NoError(t, err, "Failed to walk 'html' directory")

	// Create a new test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// We need to manually set the path for the Web handler
		r.URL.Path = "/web" + r.URL.Path
		Web(w, r)
	}))
	defer server.Close()

	for _, testFile := range testFiles {
		// WHEN
		// We make a request to the test server for this file
		url := server.URL + "/" + strings.TrimPrefix(testFile, "html/")
		resp, err := http.Get(url)
		assert.NoError(t, err, "Failed to get URL '%s'", url)

		// THEN
		// The response should be successful
		assert.Equal(t, http.StatusOK, resp.StatusCode, "for file '%s'", testFile)

		// The Content-Type should be correct
		isStaticAsset := !strings.HasSuffix(testFile, ".js") && !strings.HasSuffix(testFile, ".html")
		actualContentType := resp.Header.Get("Content-Type")

		if isStaticAsset {
			assert.NotEmpty(t, actualContentType, "Content-Type for static asset '%s' should not be empty", testFile)
			assert.NotContains(t, actualContentType, "text/plain", "Content-Type for static asset '%s' should not be text/plain", testFile)
		} else {
			expectedContentType := getContentType(testFile)
			// http.FileServer can add charset, so we check for a prefix
			assert.True(t, strings.HasPrefix(actualContentType, expectedContentType), "for file '%s', expected content type '%s', got '%s'", testFile, expectedContentType, actualContentType)
		}

		// The body should not be empty
		body, err := os.ReadFile(testFile)
		assert.NoError(t, err, "Failed to read file '%s'", testFile)
		respBody := make([]byte, len(body))
		_, err = resp.Body.Read(respBody)
		if err != nil && err.Error() != "EOF" {
			assert.NoError(t, err, "Failed to read response body for file '%s'", testFile)
		}
		assert.NotEmpty(t, respBody, "Response body should not be empty for file '%s'", testFile)

		resp.Body.Close()
	}
}

// Test that the embedded FS can be used as a http.FileSystem
func TestWebUIAsHTTPFS(t *testing.T) {
	// GIVEN
	// A http.FileSystem created from the embedded FS
	httpFS := http.FS(webUI)

	// WHEN
	// We try to open a file from the http.FileSystem
	file, err := httpFS.Open("html/index.html")

	// THEN
	// It should open the file without error
	assert.NoError(t, err, "should be able to open file from http.FS")
	if err == nil {
		file.Close()
	}
}

// Test that we can create a sub-filesystem for the "html" directory
func TestWebUISubFS(t *testing.T) {
	// GIVEN
	// A sub-filesystem for the "html" directory
	subFS, err := fs.Sub(webUI, "html")
	assert.NoError(t, err, "should be able to create sub-filesystem")

	// WHEN
	// We try to open a file from the sub-filesystem
	file, err := subFS.Open("index.html")

	// THEN
	// It should open the file without error
	assert.NoError(t, err, "should be able to open file from sub-filesystem")
	if err == nil {
		file.Close()
	}
}
