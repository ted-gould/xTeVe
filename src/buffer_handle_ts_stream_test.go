package src

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHandleTSStream(t *testing.T) {
	// 1. Setup mock server
	content := []byte("some ts stream data")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write(content)
		if err != nil {
			t.Logf("Error writing content in mock server: %v", err)
		}
	}))
	defer server.Close()

	// 2. Setup VFS and other required state
	initBufferVFS(true)
	Settings.BufferSize = 1024 // 1MB buffer size
	Settings.UserAgent = "xTeVe-Test"

	playlistID := "M1"
	streamID := 0
	tmpFolder := "/tmp/xteve_test_ts_stream/"
	err := bufferVFS.MkdirAll(tmpFolder, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() {
		if err := bufferVFS.RemoveAll(tmpFolder); err != nil {
			t.Logf("Error removing test directory %s: %v", tmpFolder, err)
		}
	}()

	md5 := getMD5(server.URL)
	stream := ThisStream{
		URL:        server.URL,
		Folder:     tmpFolder,
		PlaylistID: playlistID,
		MD5:        md5,
	}

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+md5, clients)
	defer BufferClients.Delete(playlistID + md5)

	var tmpSegment = 1
	var errors []error
	addErrorToStream := func(err error) {
		errors = append(errors, err)
	}
	var buffer = make([]byte, 1024*Settings.BufferSize)
	var bandwidth BandwidthCalculation
	var networkBandwidth = 0
	var retries = 0

	req, err := http.NewRequest("GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request to mock server: %v", err)
	}

	// 3. Call the function
	modifiedStream, err := handleTSStream(resp, stream, streamID, playlistID, tmpFolder, &tmpSegment, addErrorToStream, buffer, &bandwidth, networkBandwidth, retries)
	if err != nil {
		t.Fatalf("handleTSStream returned an error: %v", err)
	}

	// 4. Verify the results
	if !modifiedStream.Status {
		t.Errorf("Expected stream status to be true, but it was false")
	}

	if !modifiedStream.StreamFinished {
		t.Errorf("Expected stream to be finished, but it was not")
	}

	if len(errors) > 0 {
		t.Errorf("addErrorToStream was called with errors: %v", errors)
	}

	// Verify that the file was written to the VFS
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

	if !bytes.Equal(writtenContent, content) {
		t.Errorf("Content of created file does not match expected content. Got %s, want %s", string(writtenContent), string(content))
	}
}
