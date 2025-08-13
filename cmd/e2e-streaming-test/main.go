package main

import (
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
	// 10 MB of data
	streamSize = 10 * 1024 * 1024
	// Port for the streaming server
	streamingPort = 8080
	// Port for the xteve server
	xtevePort = 34400
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
	// 1. Start streamer
	fmt.Println("Starting streamer...")
	streamerCmd := exec.Command("./streamer_binary")
	streamerCmd.Env = append(os.Environ(),
		fmt.Sprintf("STREAMER_PORT=%d", streamingPort),
		fmt.Sprintf("STREAMER_SIZE=%d", streamSize),
	)
	streamerCmd.Stdout = os.Stdout
	streamerCmd.Stderr = os.Stderr
	if err := streamerCmd.Start(); err != nil {
		return fmt.Errorf("failed to start streamer: %w", err)
	}

	// Wait for streamer
	if err := waitForServerReady(fmt.Sprintf("http://localhost:%d/test.m3u", streamingPort)); err != nil {
		stopStreamer(streamerCmd)
		return fmt.Errorf("streamer not ready: %w", err)
	}

	// 2. Start xteve
	xteveCmd, err := startXteve()
	if err != nil {
		stopStreamer(streamerCmd)
		return fmt.Errorf("failed to start xteve: %w", err)
	}

	// Wait for xteve
	if err := waitForServerReady(fmt.Sprintf("http://localhost:%d/web/", xtevePort)); err != nil {
		stopStreamer(streamerCmd)
		stopXteve(xteveCmd)
		return fmt.Errorf("server not ready: %w", err)
	}

	// 3. Run tests
	testErr := runTests()

	// 4. Cleanup
	stopXteve(xteveCmd)
	stopStreamer(streamerCmd)

	if testErr != nil {
		return testErr
	}

	return nil
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

func stopStreamer(cmd *exec.Cmd) {
	fmt.Println("Stopping streamer...")
	if cmd.Process == nil {
		return
	}
	if err := cmd.Process.Kill(); err != nil {
		log.Printf("Failed to kill streamer process: %v", err)
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

func runTests() error {
	fmt.Println("Running tests...")

	// 1. Connect to the WebSocket
	wsURL := fmt.Sprintf("ws://localhost:%d/data/", xtevePort)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to websocket: %w", err)
	}
	defer conn.Close()

	// Add the M3U playlist once
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

	// Run with buffer enabled
	if err := setBuffer(conn, "xteve", 1024); err != nil {
		return err
	}
	streamURL, err := runStreamingTest(conn, true)
	if err != nil {
		return fmt.Errorf("streaming test failed with buffer enabled: %w", err)
	}
	if err := runClientDisconnectTest(streamURL, true); err != nil {
		return fmt.Errorf("client disconnect test failed with buffer enabled: %w", err)
	}
	fmt.Println("---")

	// Run with buffer disabled
	if err := setBuffer(conn, "-", 0); err != nil {
		return err
	}
	streamURL, err = runStreamingTest(conn, false)
	if err != nil {
		return fmt.Errorf("streaming test failed with buffer disabled: %w", err)
	}
	if err := runClientDisconnectTest(streamURL, false); err != nil {
		return fmt.Errorf("client disconnect test failed with buffer disabled: %w", err)
	}

	return nil
}

func getActiveConnections() (int, error) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/connections/active", streamingPort))
	if err != nil {
		return 0, fmt.Errorf("failed to get active connections: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read active connections response: %w", err)
	}

	var count int
	_, err = fmt.Sscanf(string(body), "%d", &count)
	if err != nil {
		return 0, fmt.Errorf("failed to parse active connections count: %w", err)
	}

	return count, nil
}

func runClientDisconnectTest(streamURL string, buffered bool) error {
	fmt.Println("Running client disconnect test...")

	// 1. Start streaming and disconnect early
	fmt.Printf("Streaming from %s and disconnecting early...\n", streamURL)
	streamResp, err := http.Get(streamURL)
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}
	// Read a small part of the body and then close it
	buffer := make([]byte, 1024)
	_, err = streamResp.Body.Read(buffer)
	if err != nil {
		// Reading from a closed stream will result in an error, we can ignore it
		if err != io.EOF && !strings.Contains(err.Error(), "closed") {
			return fmt.Errorf("failed to read from stream: %w", err)
		}
	}
	streamResp.Body.Close()
	fmt.Println("Client disconnected.")

	// 2. Verify that the streamer connection is closed
	time.Sleep(2 * time.Second) // Give xteve a moment to notice
	for i := 0; i < 10; i++ {
		active, err := getActiveConnections()
		if err != nil {
			return err
		}
		if active == 0 {
			fmt.Println("Streamer connection closed as expected.")
			return nil
		}
		fmt.Printf("Waiting for streamer connection to close... (active: %d)\n", active)
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("streamer connection did not close after client disconnect")
}

func setBuffer(conn *websocket.Conn, mode string, sizeKB int) error {
	fmt.Printf("Setting buffer to mode=%s, size=%dKB...\n", mode, sizeKB)
	settings := map[string]interface{}{
		"buffer":        mode,
		"buffer.size.kb": sizeKB,
	}
	request := map[string]interface{}{"cmd": "saveSettings", "settings": settings}
	if _, err := sendRequest(conn, request); err != nil {
		return fmt.Errorf("failed to set buffer settings: %w", err)
	}
	return nil
}

func runStreamingTest(conn *websocket.Conn, buffered bool) (string, error) {
	// 1. Update the M3U file in xTeVe
	fmt.Println("Updating M3U file...")
	updateRequest := map[string]interface{}{"cmd": "updateFileM3U"}
	if _, err := sendRequest(conn, updateRequest); err != nil {
		return "", fmt.Errorf("failed to send M3U update request: %w", err)
	}

	// Wait for the update to process
	time.Sleep(5 * time.Second)

	// 2. Verify the M3U output and get the stream URL
	fmt.Println("Verifying M3U output...")
	httpResp, err := http.Get(fmt.Sprintf("http://localhost:%d/m3u/xteve.m3u", xtevePort))
	if err != nil {
		return "", fmt.Errorf("failed to get M3U file: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read M3U file body: %w", err)
	}

	m3uContent := string(body)
	if !strings.Contains(m3uContent, "Test Stream") {
		return "", fmt.Errorf("verification failed: 'Test Stream' not found in M3U output")
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
		return "", fmt.Errorf("could not find stream URL in M3U output")
	}

	// 3. Stream the data and verify it
	fmt.Printf("Streaming from %s...\n", streamURL)
	streamResp, err := http.Get(streamURL)
	if err != nil {
		return "", fmt.Errorf("failed to start stream: %w", err)
	}
	defer streamResp.Body.Close()

	receivedData, err := io.ReadAll(streamResp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read stream data: %w", err)
	}

	// This now needs to call verifyStreamedData
	return streamURL, verifyStreamedData(receivedData)
}

func verifyStreamedData(data []byte) error {
	fmt.Println("Verifying streamed data...")
	expectedSize := streamSize
	if len(data) != expectedSize {
		return fmt.Errorf("streamed data size mismatch. Expected: %d, got: %d", expectedSize, len(data))
	}
	for i, b := range data {
		if b != byte(i%256) {
			return fmt.Errorf("streamed data content mismatch at byte %d. Expected: %d, got: %d", i, byte(i%256), b)
		}
	}
	fmt.Println("Streamed data verified successfully.")
	return nil
}
