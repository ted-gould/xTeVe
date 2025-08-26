package src

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestConnectWithRetry(t *testing.T) {
	t.Run("Follows Redirects", func(t *testing.T) {
		// Create a mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/redirect" {
				http.Redirect(w, r, "/target", http.StatusFound)
			} else if r.URL.Path == "/target" {
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte("Hello, world!")); err != nil {
					t.Fatalf("w.Write failed: %v", err)
				}
			} else {
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		// Setup settings for retry
		Settings.StreamRetryEnabled = false // No retries for this test

		req, _ := http.NewRequest("GET", server.URL+"/redirect", nil)
		client := &http.Client{}

		resp, err := ConnectWithRetry(client, req)
		if err != nil {
			t.Fatalf("connectWithRetry failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d, but got %d", http.StatusOK, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response body: %v", err)
		}

		if string(body) != "Hello, world!" {
			t.Errorf("Expected response body 'Hello, world!', but got '%s'", string(body))
		}
	})

	t.Run("Initial Connection", func(t *testing.T) {
		// Counter for how many times the server has been hit
		hitCount := 0

		// Create a mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hitCount++
			if hitCount <= 2 {
				// Fail the first two times
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				// Succeed the third time
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		// Setup settings for retry
		Settings.StreamRetryEnabled = true
		Settings.StreamMaxRetries = 3
		Settings.StreamRetryDelay = 1 // 1 second

		req, _ := http.NewRequest("GET", server.URL, nil)
		client := &http.Client{}

		resp, err := ConnectWithRetry(client, req)

		if err != nil {
			t.Fatalf("connectWithRetry failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d, but got %d", http.StatusOK, resp.StatusCode)
		}

		if hitCount != 3 {
			t.Errorf("Expected server to be hit 3 times, but got %d", hitCount)
		}
	})
}

func TestConnectToStreamingServer_Buffering(t *testing.T) {
	// 1. Setup mock server
	// Create 10MB of random data
	content := make([]byte, 10*1024*1024)
	for i := range content {
		content[i] = byte(i)
	}

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
	streamURL := server.URL
	channelName := "TestChannel"
	tempFolder := "/tmp/xteve_test/"
	md5 := getMD5(streamURL)
	streamFolder := tempFolder + md5 + string(os.PathSeparator)

	playlist := Playlist{
		Folder:       tempFolder,
		PlaylistID:   playlistID,
		PlaylistName: "TestPlaylist",
		Tuner:        1,
		Streams:      make(map[int]ThisStream),
		Clients:      make(map[int]ThisClient),
	}

	stream := ThisStream{
		URL:         streamURL,
		ChannelName: channelName,
		Status:      false,
		Folder:      streamFolder,
		MD5:         md5,
		PlaylistID:  playlistID,
	}
	playlist.Streams[streamID] = stream

	client := ThisClient{
		Connection: 1,
	}
	playlist.Clients[streamID] = client

	BufferInformation.Store(playlistID, playlist)

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+stream.MD5, &clients)

	// 3. Call the function to be tested
	go connectToStreamingServer(streamID, playlistID)

	// 4. Wait for buffering to happen
	// With a 1MB buffer and 10MB of data, we expect 10 files.
	expectedFiles := 10
	var foundFiles []string
	for i := 0; i < 20; i++ { // Wait for up to 20 seconds
		files, err := bufferVFS.ReadDir(streamFolder)
		if err == nil {
			if len(files) >= expectedFiles {
				for _, f := range files {
					foundFiles = append(foundFiles, f.Name())
				}
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	if len(foundFiles) < expectedFiles {
		t.Fatalf("Expected at least %d buffered files, but found %d", expectedFiles, len(foundFiles))
	}

	// 5. Verify the buffered content
	var bufferedContent []byte
	for i := 1; i <= expectedFiles; i++ {
		bufferedFilePath := streamFolder + strconv.Itoa(i) + ".ts"
		if _, err := bufferVFS.Stat(bufferedFilePath); err != nil {
			t.Fatalf("Buffered file not found: %s. Error: %v", bufferedFilePath, err)
		}

		file, err := bufferVFS.Open(bufferedFilePath)
		if err != nil {
			t.Fatalf("Failed to open buffered file: %s. Error: %v", bufferedFilePath, err)
		}

		contentPart, err := io.ReadAll(file)
		if err != nil {
			file.Close()
			t.Fatalf("Failed to read buffered file: %s. Error: %v", bufferedFilePath, err)
		}
		file.Close()
		bufferedContent = append(bufferedContent, contentPart...)
	}

	if len(bufferedContent) != len(content) {
		t.Fatalf("Buffered content length mismatch.\nExpected: %d\nGot: %d", len(content), len(bufferedContent))
	}

	for i, b := range bufferedContent {
		if b != content[i] {
			t.Fatalf("Buffered content mismatch at byte %d.\nExpected: %v\nGot: %v", i, content[i], b)
		}
	}

	// Clean up global state
	BufferInformation.Delete(playlistID)
	BufferClients.Delete(playlistID + stream.MD5)
}

func TestBufferingStream_NewStreamClientRegistration(t *testing.T) {
	// This test verifies that for a completely new stream, the client is registered
	// correctly before the connectToStreamingServer goroutine can terminate the stream.
	// This specifically tests the fix for the "instant disconnect" race condition.

	// 1. Setup mock server
	requestReceived := make(chan bool, 1)
	content := []byte("test stream data")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived <- true // Signal that connectToStreamingServer has connected
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write(content)
		if err != nil {
			t.Logf("Error writing content in mock server: %v", err)
		}
	}))
	defer server.Close()

	// 2. Setup initial state
	initBufferVFS(true)
	Settings.BufferSize = 1024
	Settings.UserAgent = "xTeVe-Test-Race"
	Settings.BufferTimeout = 0 // Start immediately
	Settings.Buffer = "xteve"  // This is required to trigger connectToStreamingServer

	playlistID := "M-NewStreamTest"
	streamURL := server.URL
	channelName := "NewStreamChannel"
	md5 := getMD5(streamURL)

	// Cleanup function to be called at the end
	defer func() {
		BufferInformation.Delete(playlistID)
		BufferClients.Delete(playlistID + md5)
	}()

	// 3. Start bufferingStream in a goroutine to simulate a client connecting
	req := httptest.NewRequest("GET", "/stream", nil)
	rr := httptest.NewRecorder()
	done := make(chan bool)
	go func() {
		bufferingStream(playlistID, streamURL, channelName, rr, req)
		done <- true
	}()

	// 4. Wait for the server to receive the request.
	// This is the critical moment. By the time the download goroutine connects to the
	// source, the client MUST have been registered.
	select {
	case <-requestReceived:
		// This is expected.
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out: Mock server never received a request.")
	}

	// 5. Verification: Check the state immediately after the download started.
	p, ok := BufferInformation.Load(playlistID)
	if !ok {
		t.Fatalf("Playlist %s was not created in BufferInformation", playlistID)
	}

	playlist, ok := p.(*Playlist)
	if !ok {
		t.Fatalf("Failed to cast to *Playlist type. Got %T", p)
	}

	if len(playlist.Streams) != 1 {
		t.Fatalf("Expected 1 stream in the playlist, but found %d", len(playlist.Streams))
	}

	c, ok := BufferClients.Load(playlistID + md5)
	if !ok {
		t.Fatal("Client was not registered in BufferClients for the new stream.")
	}

	clients, ok := c.(*ClientConnection)
	if !ok {
		t.Fatal("Failed to cast to ClientConnection type")
	}

	if clients.Connection != 1 {
		t.Fatalf("Expected client connection count to be 1, but got %d", clients.Connection)
	}

	t.Log("Successfully verified that client was registered before stream download started.")

	// 6. Wait for the client goroutine to finish
	select {
	case <-done:
		// Great, it finished.
	case <-time.After(2 * time.Second):
		t.Fatal("Test timed out: bufferingStream did not finish.")
	}
}

func TestRaceCondition_KillAndStreamEOF(t *testing.T) {
	// Channel to control the mock server's response
	unblockRead := make(chan bool)

	// 1. Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("data"))
		if err != nil {
			t.Logf("Error writing content in mock server: %v", err)
		}
		// Block until the test tells us to continue
		<-unblockRead
	}))
	defer server.Close()

	// 2. Setup state
	initBufferVFS(true)
	Settings.BufferSize = 1
	playlistID := "M-race-test"
	streamID := 0
	streamURL := server.URL
	md5 := getMD5(streamURL)

	playlist := Playlist{
		PlaylistID: playlistID,
		Streams:    make(map[int]ThisStream),
		Clients:    make(map[int]ThisClient),
		Tuner:      1,
		Folder:     "/tmp/",
	}
	stream := ThisStream{
		URL:        streamURL,
		MD5:        md5,
		PlaylistID: playlistID,
		Folder:     "/tmp/" + md5 + "/",
	}
	playlist.Streams[streamID] = stream
	BufferInformation.Store(playlistID, playlist)

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+md5, &clients)

	// 3. Start connectToStreamingServer in a goroutine
	serverDone := make(chan bool)
	go func() {
		connectToStreamingServer(streamID, playlistID)
		serverDone <- true
	}()

	time.Sleep(500 * time.Millisecond)

	// 4. Kill the connection while the server goroutine is blocked
	t.Log("Killing client connection...")
	killClientConnection(streamID, playlistID, false)

	// Verify that the stream/playlist is gone
	_, ok := BufferInformation.Load(playlistID)
	if ok {
		// With only one stream, kill should delete the whole playlist
		t.Fatal("Playlist should be deleted after killClientConnection")
	}

	// 5. Unblock the server read, which will lead to EOF
	t.Log("Unblocking server read...")
	close(unblockRead)

	<-serverDone

	// 6. Final verification: Check that the playlist has not been re-added
	_, ok = BufferInformation.Load(playlistID)
	if ok {
		t.Fatal("Playlist should NOT be re-added by the stale connectToStreamingServer goroutine")
	}
}

func TestTunerCountOnDisconnect(t *testing.T) {
	// 1. Setup
	initBufferVFS(true) // Use in-memory VFS

	playlistID := "M1"
	streamID := 0
	streamURL := "http://localhost/stream"
	channelName := "TestChannel"
	tempFolder := "/tmp/xteve_test_disconnect/"
	md5 := getMD5(streamURL)
	streamFolder := tempFolder + md5 + string(os.PathSeparator)

	// Create a dummy playlist and stream info
	playlist := Playlist{
		Folder:       tempFolder,
		PlaylistID:   playlistID,
		PlaylistName: "TestPlaylist",
		Tuner:        1,
		Streams:      make(map[int]ThisStream),
		Clients:      make(map[int]ThisClient),
	}

	stream := ThisStream{
		URL:         streamURL,
		ChannelName: channelName,
		Status:      true,
		Folder:      streamFolder,
		MD5:         md5,
		PlaylistID:  playlistID,
	}
	playlist.Streams[streamID] = stream

	client := ThisClient{
		Connection: 1,
	}
	playlist.Clients[streamID] = client

	BufferInformation.Store(playlistID, playlist)

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+md5, &clients)

	// Verify initial state
	p, _ := BufferInformation.Load(playlistID)
	if playlist, ok := p.(Playlist); ok {
		if len(playlist.Streams) != 1 {
			t.Fatalf("Initial stream count should be 1, but was %d", len(playlist.Streams))
		}
	} else {
		t.Fatalf("Failed to cast to Playlist")
	}

	// 2. Action: Simulate client disconnect
	killClientConnection(streamID, playlistID, false)

	// 3. Verification
	p, ok := BufferInformation.Load(playlistID)
	if !ok {
		// If the playlist is gone, that's also a success condition for this test
		// as it means the last stream was cleaned up.
		return
	}

	if finalPlaylist, ok := p.(Playlist); ok {
		if len(finalPlaylist.Streams) != 0 {
			t.Errorf("Expected stream count to be 0 after disconnect, but was %d", len(finalPlaylist.Streams))
		}
	} else {
		t.Fatalf("Failed to cast to Playlist")
	}

	_, clientExists := BufferClients.Load(playlistID + md5)
	if clientExists {
		t.Error("ClientConnection info should be deleted after last client disconnects")
	}
}

func TestBufferingStream_ClosesOnStreamEnd(t *testing.T) {
	// 1. Setup mock server that serves a small amount of data and then closes
	content := []byte("some finite stream data")
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
	Settings.BufferSize = 1024
	Settings.UserAgent = "xTeVe-Test"

	playlistID := "M1"
	streamID := 0
	streamURL := server.URL
	channelName := "TestChannel"
	tempFolder := "/tmp/xteve_test_closes/"
	md5 := getMD5(streamURL)
	streamFolder := tempFolder + md5 + string(os.PathSeparator)

	playlist := Playlist{
		Folder:       tempFolder,
		PlaylistID:   playlistID,
		PlaylistName: "TestPlaylist",
		Tuner:        1,
		Streams:      make(map[int]ThisStream),
		Clients:      make(map[int]ThisClient),
	}

	stream := ThisStream{
		URL:         streamURL,
		ChannelName: channelName,
		Status:      false,
		Folder:      streamFolder,
		MD5:         md5,
		PlaylistID:  playlistID,
	}
	playlist.Streams[streamID] = stream

	client := ThisClient{
		Connection: 1,
	}
	playlist.Clients[streamID] = client

	BufferInformation.Store(playlistID, playlist)

	var clients ClientConnection
	clients.Connection = 1
	BufferClients.Store(playlistID+stream.MD5, &clients)

	// 3. Start buffering in a goroutine
	go connectToStreamingServer(streamID, playlistID)

	// 4. Call bufferingStream and check if it closes
	req := httptest.NewRequest("GET", "/stream", nil)
	rr := httptest.NewRecorder()

	done := make(chan bool)
	go func() {
		bufferingStream(playlistID, streamURL, channelName, rr, req)
		done <- true
	}()

	select {
	case <-done:
		// Test passed, bufferingStream finished
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out: bufferingStream did not close after stream ended")
	}

	// 5. Verify the received content
	resp := rr.Result()
	body, _ := io.ReadAll(resp.Body)

	if string(body) != string(content) {
		t.Errorf("Expected response body '%s', but got '%s'", string(content), string(body))
	}

	// Clean up
	BufferInformation.Delete(playlistID)
	BufferClients.Delete(playlistID + stream.MD5)
}
