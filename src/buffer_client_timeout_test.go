package src

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type MockResponseWriter struct {
	headers      http.Header
	writeCalled  bool
	deadlineSet  bool
	deadline     time.Time
	writeSleep   time.Duration
}

func (m *MockResponseWriter) Header() http.Header {
	if m.headers == nil {
		m.headers = make(http.Header)
	}
	return m.headers
}

func (m *MockResponseWriter) Write(b []byte) (int, error) {
	m.writeCalled = true
	// Check deadline
	if m.deadlineSet && !m.deadline.IsZero() {
		if time.Now().After(m.deadline) {
			return 0, os.ErrDeadlineExceeded
		}
		// Simulate blocking write that exceeds deadline
		if m.writeSleep > 0 {
			time.Sleep(m.writeSleep)
			if time.Now().After(m.deadline) {
				return 0, os.ErrDeadlineExceeded
			}
		}
	}
	return len(b), nil
}

func (m *MockResponseWriter) WriteHeader(statusCode int) {}

// Implement SetWriteDeadline for ResponseController
func (m *MockResponseWriter) SetWriteDeadline(t time.Time) error {
	m.deadlineSet = true
	m.deadline = t
	return nil
}

func (m *MockResponseWriter) Flush() {}

// TestBufferClientTimeout verifies that the server closes the connection
// if the client doesn't read data within the configured timeout.
func TestBufferClientTimeout(t *testing.T) {
	// 1. Setup
	// Set the environment variable to a short timeout for testing (e.g., 50ms)
	os.Setenv("XTEVE_BUFFER_CLIENT_TIMEOUT", "50")
	defer os.Unsetenv("XTEVE_BUFFER_CLIENT_TIMEOUT")

	// Initialize System and Settings
	Settings = SettingsStruct{}
	System = SystemStruct{}
	System.Folder.Config = t.TempDir() + string(os.PathSeparator)
	System.Folder.Temp = t.TempDir() + string(os.PathSeparator)
	System.Folder.Data = System.Folder.Config + "data" + string(os.PathSeparator)
	System.Folder.Backup = System.Folder.Config + "backup" + string(os.PathSeparator)
	System.Folder.Cache = System.Folder.Config + "cache" + string(os.PathSeparator)

	// Ensure folders exist
	assert.NoError(t, os.MkdirAll(System.Folder.Data, 0755))
	assert.NoError(t, os.MkdirAll(System.Folder.Backup, 0755))
	assert.NoError(t, os.MkdirAll(System.Folder.Cache, 0755))

	// Mock necessary system state
	// Create a dummy settings file
	System.File.Settings = System.Folder.Config + "settings.json"
	assert.NoError(t, saveMapToJSONFile(System.File.Settings, map[string]interface{}{}))

	// Load settings (this should pick up the env var)
	_, err := loadSettings()
	assert.NoError(t, err)

	assert.Equal(t, 50.0, Settings.BufferClientTimeout, "BufferClientTimeout should be 50 from env var")

	// Mock a playlist and stream
	// Use unique ID to avoid global state pollution from previous runs
	playlistID := fmt.Sprintf("MOCK_PLAYLIST_%d", time.Now().UnixNano())
	streamID := 0

	// Setup BufferInformation
	playlist := &Playlist{
		PlaylistID: playlistID,
		Streams:    make(map[int]ThisStream),
		Clients:    make(map[int]ThisClient),
		Tuner:      10,
		Folder:     System.Folder.Temp + playlistID + string(os.PathSeparator),
	}
	// Setup in-memory VFS for buffer
	initBufferVFS(true)
	assert.NoError(t, checkVFSFolder(playlist.Folder, bufferVFS))

	stream := ThisStream{
		URL:          "http://mock/stream.ts",
		Status:       true, // Pretend it's already active
		MD5:          "MOCK_MD5",
		Folder:       playlist.Folder + "MOCK_MD5" + string(os.PathSeparator),
		PlaylistID:   playlistID,
		PlaylistName: "Mock Playlist",
	}
	assert.NoError(t, checkVFSFolder(stream.Folder, bufferVFS))

	playlist.Streams[streamID] = stream
	BufferInformation.Store(playlistID, playlist)

	// Mock a client connection with 0 count so it can be cleaned up
	// We assume reserveStreamSlot will increment it to 1.
	clients := &ClientConnection{Connection: 0}
	BufferClients.Store(playlistID+stream.MD5, clients)

	// Create a dummy segment file so bufferingStream has something to send
	segmentName := "0.ts"
	f, _ := bufferVFS.Create(stream.Folder + segmentName)
	_, err = f.Write(make([]byte, 1024)) // 1KB of data
	assert.NoError(t, err)
	f.Close()

	// Update completed segments
	stream.CompletedSegments = append(stream.CompletedSegments, SegmentInfo{Filename: segmentName})
	playlist.Streams[streamID] = stream
	BufferInformation.Store(playlistID, playlist)

	// Mock ResponseWriter that sleeps longer than timeout
	mockW := &MockResponseWriter{
		writeSleep: 100 * time.Millisecond, // Sleep 100ms, timeout is 50ms
	}

	req, _ := http.NewRequest("GET", "http://mock", nil)

	// Run bufferingStream
	// It should try to write, block (sleep), see deadline exceeded, and return.

	done := make(chan bool)
	go func() {
		bufferingStream(playlistID, "http://mock/stream.ts", "Mock Channel", mockW, req)
		done <- true
	}()

	select {
	case <-done:
		// Success
		assert.True(t, mockW.writeCalled, "Write should have been called")
		assert.True(t, mockW.deadlineSet, "SetWriteDeadline should have been called")
	case <-time.After(5 * time.Second):
		t.Fatal("bufferingStream did not return within timeout")
	}

	// Verify client connection was killed and removed
	// Since we used unique ID, this check is safe from pollution
	_, ok := BufferClients.Load(playlistID + stream.MD5)
	assert.False(t, ok, "Client connection should have been removed")
}
