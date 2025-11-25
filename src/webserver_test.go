package src

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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


func TestJSTemplate(t *testing.T) {
	// GIVEN
	// A new test server
	server := httptest.NewServer(http.HandlerFunc(Web))
	defer server.Close()

	// WHEN
	// We make a request to the test server for a JS file that contains a template
	url := server.URL + "/web/js/settings_ts.js"
	resp, err := http.Get(url)
	assert.NoError(t, err, "Failed to get URL '%s'", url)
	defer resp.Body.Close()

	// THEN
	// The response should be successful
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// The response body should contain the templated string
	bodyBytes, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	bodyString := string(bodyBytes)
	assert.NotContains(t, bodyString, "{{.settings.update.title}}")
	assert.Contains(t, bodyString, "Schedule for updating (Playlist, XMLTV, Backup)")
}

func TestTracingMiddleware(t *testing.T) {
	// Create an in-memory exporter
	exporter := tracetest.NewInMemoryExporter()

	// Create a new tracer provider with the in-memory exporter
	tp := trace.NewTracerProvider(trace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)

	// Create a new test server with the handler
	handler := newHTTPHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	// Make a request to the test server
	req, _ := http.NewRequest("GET", server.URL, nil)
	client := &http.Client{}
	_, err := client.Do(req)
	assert.NoError(t, err)

	// Force flush the exporter
	err = tp.ForceFlush(context.Background())
	assert.NoError(t, err)

	// Check that a span was created
	spans := exporter.GetSpans()
	var found bool
	for _, span := range spans {
		if span.Name == "/" {
			found = true
			break
		}
	}
	assert.True(t, found, "Index span not found")

	// Shut down the tracer provider
	err = tp.Shutdown(context.Background())
	assert.NoError(t, err)
}
