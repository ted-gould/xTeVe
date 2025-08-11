package src

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMimeTypeJS(t *testing.T) {
	expectedMimeType := "application/javascript"
	actualMimeType := mime.TypeByExtension(".js")

	if actualMimeType != expectedMimeType {
		t.Errorf("Expected MIME type for .js to be %s, but got %s", expectedMimeType, actualMimeType)
	}
}

func TestJSFileMimeTypeE2E(t *testing.T) {
	// GIVEN
	// A new test server
	server := httptest.NewServer(http.HandlerFunc(Web))
	defer server.Close()

	// A list of JS files to test
	jsFiles := []string{
		"/web/js/network_ts.js",
		"/web/js/authentication_ts.js",
		"/web/js/base_ts.js",
		"/web/js/configuration_ts.js",
		"/web/js/logs_ts.js",
		"/web/js/menu_ts.js",
		"/web/js/settings_ts.js",
	}

	for _, jsFile := range jsFiles {
		// WHEN
		// We make a request to the test server for a JS file
		url := server.URL + jsFile
		resp, err := http.Get(url)
		assert.NoError(t, err, "Failed to get URL '%s'", url)
		defer resp.Body.Close()

		// THEN
		// The response should be successful
		assert.Equal(t, http.StatusOK, resp.StatusCode, "for file '%s'", jsFile)

		// The Content-Type should be exactly "application/javascript"
		expectedContentType := "application/javascript"
		actualContentType := resp.Header.Get("Content-Type")
		assert.Equal(t, expectedContentType, actualContentType, "for file '%s', unexpected content type", jsFile)
	}
}
