package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// 50 MB of data
	streamSize = 50 * 1024 * 1024
	// Port for the streaming server
	streamingPort = 8080
	// Port for the xteve server
	xtevePort = 34400
)

var (
	// Holds the generated data for verification
	testData []byte
	m3uPath  string
)

// WebSocketResponse defines the structure of a response from the server.
type WebSocketResponse struct {
	Status bool   `json:"status"`
	Error  string `json:"err,omitempty"`
}

func main() {
	if err := run(); err != nil {
		log.Printf("E2E test failed: %v", err)
		os.Exit(1)
	}
	fmt.Println("E2E test completed successfully!")
}

func run() error {
	// 1. Generate the test data
	if err := generateTestData(); err != nil {
		return fmt.Errorf("failed to generate test data: %w", err)
	}

	// 2. Create the M3U file
	if err := createTempM3UFile(); err != nil {
		return fmt.Errorf("failed to create temp m3u file: %w", err)
	}
	defer os.Remove(m3uPath)

	// 3. Start the streaming server
	go startStreamingServer()

	// 4. Start the xteve server
	cmd, err := startXteve()
	if err != nil {
		return fmt.Errorf("failed to start xteve: %w", err)
	}
	defer stopXteve(cmd)

	// Wait for the server to be ready
	if err := waitForServerReady(fmt.Sprintf("http://localhost:%d/web/", xtevePort)); err != nil {
		return fmt.Errorf("server not ready: %w", err)
	}

	// 5. Run the tests
	if err := runTests(); err != nil {
		return fmt.Errorf("tests failed: %w", err)
	}

	return nil
}

func generateTestData() error {
	fmt.Println("Generating test data...")
	testData = make([]byte, streamSize)
	if _, err := rand.Read(testData); err != nil {
		return fmt.Errorf("failed to generate random data: %w", err)
	}
	return nil
}

func startStreamingServer() {
	fmt.Printf("Starting streaming server on port %d...\n", streamingPort)
	http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mpeg")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", streamSize))
		w.Write(testData)
	})
	http.HandleFunc("/test.m3u", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, m3uPath)
	})
	if err := http.ListenAndServe(fmt.Sprintf(":%d", streamingPort), nil); err != nil {
		log.Fatalf("Streaming server failed: %v", err)
	}
}

func startXteve() (*exec.Cmd, error) {
	fmt.Println("Starting xteve server...")
	// Remove existing config to ensure a clean slate
	os.RemoveAll(".xteve")

	cmd := exec.Command("./bin/xteve", fmt.Sprintf("-port=%d", xtevePort), "-config=.xteve")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func stopXteve(cmd *exec.Cmd) {
	fmt.Println("Stopping xteve server...")
	if cmd.Process == nil {
		return
	}
	if err := cmd.Process.Kill(); err != nil {
		log.Printf("Failed to kill xteve process: %v", err)
	}
}

func waitForServerReady(url string) error {
	fmt.Println("Waiting for server to be ready...")
	for i := 0; i < 30; i++ {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			fmt.Println("Server is ready.")
			resp.Body.Close()
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("server is not ready after 30 seconds")
}

// sendRequest sends a JSON request to the WebSocket and returns the server's response.
func sendRequest(conn *websocket.Conn, request map[string]interface{}) (*WebSocketResponse, error) {
	if err := conn.WriteJSON(request); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response WebSocketResponse
	if err := json.Unmarshal(msg, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &response, nil
}

func createTempM3UFile() error {
	m3uContent := fmt.Sprintf(`#EXTM3U
#EXTINF:-1 tvg-id="test.stream" tvg-name="Test Stream" group-title="Test",Test Stream
http://localhost:%d/stream
`, streamingPort)

	tmpfile, err := os.CreateTemp("", "test.m3u")
	if err != nil {
		return fmt.Errorf("failed to create temp m3u file: %w", err)
	}

	if _, err := tmpfile.Write([]byte(m3uContent)); err != nil {
		tmpfile.Close()
		return fmt.Errorf("failed to write to temp m3u file: %w", err)
	}

	if err := tmpfile.Close(); err != nil {
		return fmt.Errorf("failed to close temp m3u file: %w", err)
	}

	m3uPath = tmpfile.Name()
	return nil
}

func runTests() error {
	fmt.Println("Running tests...")

	// 1. Connect to the WebSocket
	wsURL := fmt.Sprintf("ws://localhost:%d/data/", xtevePort)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to websocket: %w", err)
	}
	defer conn.Close()

	// 2. Add the M3U playlist
	m3uURL := fmt.Sprintf("http://localhost:%d/test.m3u", streamingPort)
	fmt.Printf("Adding M3U playlist from %s...\n", m3uURL)
	m3uData := map[string]interface{}{
		"-": map[string]interface{}{
			"name": "TestM3U",
			"url":  m3uURL,
		},
	}
	m3uRequest := map[string]interface{}{"cmd": "saveFilesM3U", "files": map[string]interface{}{"m3u": m3uData}}
	if _, err := sendRequest(conn, m3uRequest); err != nil {
		return fmt.Errorf("failed to add M3U playlist: %w", err)
	}

	// 3. Update the M3U file in xTeVe
	fmt.Println("Updating M3U file...")
	updateRequest := map[string]interface{}{"cmd": "updateFileM3U"}
	if _, err := sendRequest(conn, updateRequest); err != nil {
		return fmt.Errorf("failed to send M3U update request: %w", err)
	}

	// Wait for the update to process
	time.Sleep(5 * time.Second)

	// 4. Verify the M3U output and get the stream URL
	fmt.Println("Verifying M3U output...")
	httpResp, err := http.Get(fmt.Sprintf("http://localhost:%d/m3u/xteve.m3u", xtevePort))
	if err != nil {
		return fmt.Errorf("failed to get M3U file: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read M3U file body: %w", err)
	}

	m3uContent := string(body)
	if !strings.Contains(m3uContent, "Test Stream") {
		return fmt.Errorf("verification failed: 'Test Stream' not found in M3U output")
	}

	// Find the stream URL from the M3U content
	var streamURL string
	lines := strings.Split(m3uContent, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Test Stream") && i+1 < len(lines) {
			streamURL = lines[i+1]
			break
		}
	}

	if streamURL == "" {
		return fmt.Errorf("could not find stream URL in M3U output")
	}

	// 5. Stream the data and verify it
	fmt.Printf("Streaming from %s...\n", streamURL)
	streamResp, err := http.Get(streamURL)
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}
	defer streamResp.Body.Close()

	receivedData, err := io.ReadAll(streamResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read stream data: %w", err)
	}

	fmt.Println("Verifying streamed data...")
	if !bytes.Equal(testData, receivedData) {
		return fmt.Errorf("streamed data does not match original data. Expected size: %d, got: %d", len(testData), len(receivedData))
	}

	fmt.Println("Streamed data verified successfully.")
	return nil
}
