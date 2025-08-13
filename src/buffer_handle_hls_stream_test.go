package src

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestHandleHLSStream(t *testing.T) {
	// 1. Setup mock server
	tsContent := "some ts segment data"
	m3u8Playlist := fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXTINF:10.0,\nsegment1.ts\n#EXT-X-ENDLIST")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(m3u8Playlist))
			if err != nil {
				t.Logf("Error writing m3u8 playlist in mock server: %v", err)
			}
		} else if strings.HasSuffix(r.URL.Path, ".ts") {
			w.Header().Set("Content-Type", "video/mp2t")
			w.WriteHeader(http.StatusOK)
			_, err := w.Write([]byte(tsContent))
			if err != nil {
				t.Logf("Error writing ts content in mock server: %v", err)
			}
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// 2. Setup VFS and other required state
	initBufferVFS(true)
	Settings.UserAgent = "xTeVe-Test"
	tmpFolder := "/tmp/xteve_test_hls_stream/"
	err := bufferVFS.MkdirAll(tmpFolder, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() {
		if err := bufferVFS.RemoveAll(tmpFolder); err != nil {
			t.Logf("Error removing test directory %s: %v", tmpFolder, err)
		}
	}()

	// The initial response is for the m3u8 playlist itself
	resp, err := http.Get(server.URL + "/playlist.m3u8")
	if err != nil {
		t.Fatalf("Failed to make request to mock server for m3u8: %v", err)
	}

	stream := ThisStream{
		URL:                server.URL + "/playlist.m3u8",
		URLStreamingServer: server.URL,
		Folder:             tmpFolder,
	}

	var tmpSegment = 1
	var errors []error
	addErrorToStream := func(err error) {
		errors = append(errors, err)
	}

	// 3. Call the function
	modifiedStream, err := handleHLSStream(resp, stream, tmpFolder, &tmpSegment, addErrorToStream, stream.URL)
	if err != nil {
		t.Fatalf("handleHLSStream returned an error: %v", err)
	}

	// 4. Verify the results
	if !modifiedStream.HLS {
		t.Errorf("Expected stream to be marked as HLS, but it was not")
	}

	if len(errors) > 0 {
		t.Errorf("addErrorToStream was called with errors: %v", errors)
	}

	// Verify that the segment file was written to the VFS
	expectedFile := tmpFolder + "1.ts"
	if _, err := bufferVFS.Stat(expectedFile); os.IsNotExist(err) {
		t.Fatalf("Expected file %s to be created, but it was not", expectedFile)
	}

	fileContent, err := bufferVFS.Open(expectedFile)
	if err != nil {
		t.Fatalf("Failed to open created file: %v", err)
	}
	defer fileContent.Close()

	writtenContent, err := io.ReadAll(fileContent)
	if err != nil {
		t.Fatalf("Failed to read content of created file: %v", err)
	}

	if !bytes.Equal(writtenContent, []byte(tsContent)) {
		t.Errorf("Content of created file does not match expected content. Got %s, want %s", string(writtenContent), tsContent)
	}

	if tmpSegment != 2 {
		t.Errorf("Expected tmpSegment to be 2, but got %d", tmpSegment)
	}
}
