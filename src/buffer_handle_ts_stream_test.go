package src

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"xteve/src/mpegts"
)

func TestHandleTSStream(t *testing.T) {
	// 1. Setup mock server
	var validTSStream bytes.Buffer
	packet1 := make([]byte, mpegts.PacketSize)
	packet1[0] = mpegts.SyncByte
	validTSStream.Write(packet1)
	packet2 := make([]byte, mpegts.PacketSize)
	packet2[0] = mpegts.SyncByte
	validTSStream.Write(packet2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write(validTSStream.Bytes())
		if err != nil {
			t.Logf("Error writing content in mock server: %v", err)
		}
	}))
	defer server.Close()

	// 2. Setup VFS and other required state
	initBufferVFS(true)
	Settings.BufferSize = 1024 // 1MB buffer size
	Settings.UserAgent = "xTeVe-Test"
	Settings.StreamRetryEnabled = false // EOF should finish the stream in this test

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

	md5, err := getMD5(server.URL)
	if err != nil {
		t.Fatalf("getMD5 failed: %v", err)
	}
	stream := ThisStream{
		URL:        server.URL,
		Folder:     tmpFolder,
		PlaylistID: playlistID,
		MD5:        md5,
	}

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+md5, &clients)
	defer BufferClients.Delete(playlistID + md5)

	var tmpSegment = 1
	var errors []error
	addErrorToStream := func(err error) {
		errors = append(errors, err)
	}
	var buffer = make([]byte, 1024*Settings.BufferSize)
	var bandwidth BandwidthCalculation
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
	modifiedStream, err := handleTSStream(resp, stream, streamID, playlistID, tmpFolder, &tmpSegment, addErrorToStream, buffer, &bandwidth, retries)
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

	if !bytes.Equal(writtenContent, validTSStream.Bytes()) {
		t.Errorf("Content of created file does not match expected content. Got %s, want %s", string(writtenContent), validTSStream.String())
	}
}

func TestHandleTSStream_Corrupted(t *testing.T) {
	// 1. Setup mock server with corrupted data
	var corruptedTSStream bytes.Buffer
	corruptedTSStream.Write([]byte{0x01, 0x02, 0x03}) // Garbage
	packet1 := make([]byte, mpegts.PacketSize)
	packet1[0] = mpegts.SyncByte
	corruptedTSStream.Write(packet1)
	corruptedTSStream.Write([]byte{0x04, 0x05, 0x06}) // More garbage
	packet2 := make([]byte, mpegts.PacketSize)
	packet2[0] = mpegts.SyncByte
	corruptedTSStream.Write(packet2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write(corruptedTSStream.Bytes())
		if err != nil {
			t.Logf("Error writing content in mock server: %v", err)
		}
	}))
	defer server.Close()

	// 2. Setup VFS and other required state
	initBufferVFS(true)
	Settings.BufferSize = 1024 // 1MB buffer size
	Settings.UserAgent = "xTeVe-Test"
	Settings.StreamRetryEnabled = false // EOF should finish the stream in this test

	playlistID := "M1"
	streamID := 0
	tmpFolder := "/tmp/xteve_test_ts_stream_corrupted/"
	err := bufferVFS.MkdirAll(tmpFolder, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() {
		if err := bufferVFS.RemoveAll(tmpFolder); err != nil {
			t.Logf("Error removing test directory %s: %v", tmpFolder, err)
		}
	}()

	md5, err := getMD5(server.URL)
	if err != nil {
		t.Fatalf("getMD5 failed: %v", err)
	}
	stream := ThisStream{
		URL:        server.URL,
		Folder:     tmpFolder,
		PlaylistID: playlistID,
		MD5:        md5,
	}

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+md5, &clients)
	defer BufferClients.Delete(playlistID + md5)

	var tmpSegment = 1
	var errors []error
	addErrorToStream := func(err error) {
		errors = append(errors, err)
	}
	var buffer = make([]byte, 1024*Settings.BufferSize)
	var bandwidth BandwidthCalculation
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
	_, err = handleTSStream(resp, stream, streamID, playlistID, tmpFolder, &tmpSegment, addErrorToStream, buffer, &bandwidth, retries)
	if err != nil {
		t.Fatalf("handleTSStream returned an error: %v", err)
	}

	// 4. Verify the results
	if len(errors) > 0 {
		t.Errorf("addErrorToStream was called with errors: %v", errors)
	}

	// Verify that the file was written to the VFS and contains only the valid packets
	expectedFile := tmpFolder + "1.ts"
	fileContent, err := bufferVFS.Open(expectedFile)
	if err != nil {
		t.Fatalf("Failed to open created file: %v", err)
	}
	defer fileContent.Close()

	writtenContent, err := io.ReadAll(fileContent)
	if err != nil {
		t.Fatalf("Failed to read content of created file: %v", err)
	}

	var expectedContent bytes.Buffer
	expectedContent.Write(packet1)
	expectedContent.Write(packet2)

	if !bytes.Equal(writtenContent, expectedContent.Bytes()) {
		t.Errorf("Content of created file does not match expected content. Got %d bytes, want %d bytes", len(writtenContent), len(expectedContent.Bytes()))
	}
}

func TestHandleTSStream_EOFRetriesWhenEnabled(t *testing.T) {
	// Simulate an upstream server that sends a small amount of valid TS data
	// then closes the connection (EOF). With retry enabled, handleTSStream
	// should return a "redirect" error to trigger reconnection instead of
	// marking the stream as finished.
	var validTSStream bytes.Buffer
	packet := make([]byte, mpegts.PacketSize)
	packet[0] = mpegts.SyncByte
	validTSStream.Write(packet)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validTSStream.Bytes())
		// Connection closes here, causing EOF on the client side
	}))
	defer server.Close()

	initBufferVFS(true)
	origBufferSize := Settings.BufferSize
	origRetryEnabled := Settings.StreamRetryEnabled
	origMaxRetries := Settings.StreamMaxRetries
	origRetryDelay := Settings.StreamRetryDelay
	defer func() {
		Settings.BufferSize = origBufferSize
		Settings.StreamRetryEnabled = origRetryEnabled
		Settings.StreamMaxRetries = origMaxRetries
		Settings.StreamRetryDelay = origRetryDelay
	}()

	Settings.BufferSize = 1024
	Settings.UserAgent = "xTeVe-Test"
	Settings.StreamRetryEnabled = true
	Settings.StreamMaxRetries = 3
	Settings.StreamRetryDelay = 0 // No delay in tests

	playlistID := "M1-eof-retry"
	streamID := 0
	tmpFolder := "/tmp/xteve_test_ts_eof_retry/"
	if err := bufferVFS.MkdirAll(tmpFolder, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() {
		_ = bufferVFS.RemoveAll(tmpFolder)
	}()

	md5Val, err := getMD5(server.URL)
	if err != nil {
		t.Fatalf("getMD5 failed: %v", err)
	}
	stream := ThisStream{
		URL:        server.URL,
		Folder:     tmpFolder,
		PlaylistID: playlistID,
		MD5:        md5Val,
	}

	// Setup BufferInformation so completeTSsegment can update it
	playlist := &Playlist{
		Streams: map[int]ThisStream{streamID: stream},
	}
	BufferInformation.Store(playlistID, playlist)
	defer BufferInformation.Delete(playlistID)

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+md5Val, &clients)
	defer BufferClients.Delete(playlistID + md5Val)

	var tmpSegment = 1
	var streamErrors []error
	addErrorToStream := func(err error) {
		streamErrors = append(streamErrors, err)
	}
	var buffer = make([]byte, 1024*Settings.BufferSize)
	var bandwidth BandwidthCalculation

	req, err := http.NewRequest("GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	resultStream, err := handleTSStream(resp, stream, streamID, playlistID, tmpFolder, &tmpSegment, addErrorToStream, buffer, &bandwidth, 0)
	if err == nil {
		t.Fatalf("Expected 'redirect' error for EOF retry, got nil (stream finished prematurely). StreamFinished=%v", resultStream.StreamFinished)
	}
	if err.Error() != "redirect" {
		t.Fatalf("Expected 'redirect' error, got: %v", err)
	}

	// Stream should NOT be marked as finished since we're retrying
	if resultStream.StreamFinished {
		t.Errorf("Stream should not be marked as finished when retrying on EOF")
	}

	if len(streamErrors) > 0 {
		t.Errorf("addErrorToStream should not have been called, got: %v", streamErrors)
	}
}

func TestHandleTSStream_EOFNoRetryWhenDisabled(t *testing.T) {
	// With retry disabled, EOF should mark the stream as finished (original behavior).
	var validTSStream bytes.Buffer
	packet := make([]byte, mpegts.PacketSize)
	packet[0] = mpegts.SyncByte
	validTSStream.Write(packet)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validTSStream.Bytes())
	}))
	defer server.Close()

	initBufferVFS(true)
	origBufferSize := Settings.BufferSize
	origRetryEnabled := Settings.StreamRetryEnabled
	defer func() {
		Settings.BufferSize = origBufferSize
		Settings.StreamRetryEnabled = origRetryEnabled
	}()

	Settings.BufferSize = 1024
	Settings.UserAgent = "xTeVe-Test"
	Settings.StreamRetryEnabled = false

	playlistID := "M1-eof-noretry"
	streamID := 0
	tmpFolder := "/tmp/xteve_test_ts_eof_noretry/"
	if err := bufferVFS.MkdirAll(tmpFolder, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() {
		_ = bufferVFS.RemoveAll(tmpFolder)
	}()

	md5Val, err := getMD5(server.URL)
	if err != nil {
		t.Fatalf("getMD5 failed: %v", err)
	}
	stream := ThisStream{
		URL:        server.URL,
		Folder:     tmpFolder,
		PlaylistID: playlistID,
		MD5:        md5Val,
	}

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+md5Val, &clients)
	defer BufferClients.Delete(playlistID + md5Val)

	var tmpSegment = 1
	var streamErrors []error
	addErrorToStream := func(err error) {
		streamErrors = append(streamErrors, err)
	}
	var buffer = make([]byte, 1024*Settings.BufferSize)
	var bandwidth BandwidthCalculation

	req, err := http.NewRequest("GET", server.URL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	resultStream, err := handleTSStream(resp, stream, streamID, playlistID, tmpFolder, &tmpSegment, addErrorToStream, buffer, &bandwidth, 0)
	if err != nil {
		t.Fatalf("handleTSStream returned unexpected error: %v", err)
	}

	if !resultStream.StreamFinished {
		t.Errorf("Expected stream to be finished when retry is disabled")
	}

	if !resultStream.Status {
		t.Errorf("Expected stream status to be true")
	}

	if len(streamErrors) > 0 {
		t.Errorf("addErrorToStream should not have been called, got: %v", streamErrors)
	}
}
