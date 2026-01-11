package src

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestAPIStatusHandler_Tuners(t *testing.T) {
	// Clear BufferInformation to ensure clean state
	BufferInformation.Range(func(key, value any) bool {
		BufferInformation.Delete(key)
		return true
	})

	// Setup a dummy playlist with active streams
	playlistID := "TEST_PLAYLIST_API_STATUS"
	playlist := &Playlist{
		PlaylistID: playlistID,
		Tuner:      5,
		Streams:    make(map[int]ThisStream),
		Clients:    make(map[int]ThisClient),
	}

	// Add a stream to simulate active tuner usage
	playlist.Streams[0] = ThisStream{
		ChannelName: "Test Channel API Status",
		Status:      true,
	}

	// Store it in BufferInformation
	BufferInformation.Store(playlistID, playlist)

	// Prepare API Request
	reqBody := APIRequestStruct{
		Cmd: "status",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/", bytes.NewBuffer(bodyBytes))
	// Mock IP address to loopback to pass security check
	req.RemoteAddr = "127.0.0.1:12345"

	w := httptest.NewRecorder()

	// Call API
	API(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var apiResp APIResponseStruct
	err := json.NewDecoder(resp.Body).Decode(&apiResp)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify that TunerActive correctly reflects the active stream
	if apiResp.TunerActive != 1 {
		t.Errorf("Expected TunerActive 1, got %d", apiResp.TunerActive)
	}
	// Verify that TunerAll correctly reflects the playlist capacity
	if apiResp.TunerAll != 5 {
		t.Errorf("Expected TunerAll 5, got %d", apiResp.TunerAll)
	}
}
