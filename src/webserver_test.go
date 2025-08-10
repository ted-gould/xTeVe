package src

import (
	"mime"
	"testing"
)

func TestMimeTypeJS(t *testing.T) {
	expectedMimeType := "application/javascript"
	actualMimeType := mime.TypeByExtension(".js")

	if actualMimeType != expectedMimeType {
		t.Errorf("Expected MIME type for .js to be %s, but got %s", expectedMimeType, actualMimeType)
	}
}
