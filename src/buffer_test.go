package src

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

		resp, err := connectWithRetry(client, req)
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

		resp, err := connectWithRetry(client, req)

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

func TestGetBufTmpFiles(t *testing.T) {
	// Initialize the in-memory filesystem for the test
	initBufferVFS(true)

	// Setup a dummy stream and directory
	stream := &ThisStream{
		Folder:      "/tmp/stream1/",
		OldSegments: []string{},
	}
	err := bufferVFS.MkdirAll(stream.Folder, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() {
		if err := bufferVFS.RemoveAll(stream.Folder); err != nil {
			t.Logf("Error removing test directory %s: %v", stream.Folder, err)
		}
	}()

	// Create dummy segment files
	dummyFiles := []string{"1.ts", "2.ts", "3.ts"}
	for _, fname := range dummyFiles {
		file, err := bufferVFS.Create(stream.Folder + fname)
		if err != nil {
			t.Fatalf("Failed to create dummy file %s: %v", fname, err)
		}
		file.Close()
	}

	// Call the function to test
	tmpFiles := getBufTmpFiles(stream)

	// The function should return all available segment files.
	if len(tmpFiles) != len(dummyFiles) {
		t.Fatalf("Expected %d files, but got %d", len(dummyFiles), len(tmpFiles))
	}

	for i, f := range tmpFiles {
		if f != dummyFiles[i] {
			t.Errorf("Expected file %s at index %d, but got %s", dummyFiles[i], i, f)
		}
	}
}

func TestConnectToStreamingServer_Buffering(t *testing.T) {
	// 1. Setup mock server
	content := "0123456789abcdef" // 16 bytes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(content))
		if err != nil {
			t.Logf("Error writing content in mock server: %v", err)
		}
	}))
	defer server.Close()

	// 2. Setup VFS and other required state
	initBufferVFS(true)
	Settings.BufferSize = 1 // 1 KB, so our content should be smaller than one segment file
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
	BufferClients.Store(playlistID+stream.MD5, clients)

	// 3. Call the function to be tested
	go connectToStreamingServer(streamID, playlistID)

	// 4. Wait for buffering to happen (give it a moment)
	time.Sleep(2 * time.Second)

	// 5. Verify the buffered content
	// The first segment file should be named "1.ts"
	bufferedFilePath := streamFolder + "1.ts"

	// Check if the file exists
	if _, err := bufferVFS.Stat(bufferedFilePath); err != nil {
		t.Fatalf("Buffered file not found: %s. Error: %v", bufferedFilePath, err)
	}

	file, err := bufferVFS.Open(bufferedFilePath)
	if err != nil {
		t.Fatalf("Failed to open buffered file: %s. Error: %v", bufferedFilePath, err)
	}
	defer file.Close()

	bufferedContent, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("Failed to read buffered file: %s. Error: %v", bufferedFilePath, err)
	}

	if strings.TrimSpace(string(bufferedContent)) != content {
		t.Errorf("Buffered content mismatch.\nExpected: %s\nGot: %s", content, string(bufferedContent))
	}

	// Clean up global state
	BufferInformation.Delete(playlistID)
	BufferClients.Delete(playlistID + stream.MD5)
}
