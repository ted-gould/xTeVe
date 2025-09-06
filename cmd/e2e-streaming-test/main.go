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
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"xteve/src/mpegts"
)

const (
	// 1 MB of data
	streamSize = 1 * 1024 * 1024
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

// StatusResponse defines the structure for the /api/status response.
type StatusResponse struct {
	TunerActive int `json:"tuners.active"`
}

func main() {
	if err := run(); err != nil {
		log.Printf("E2E test failed: %v", err)
		os.Exit(1)
	}
	fmt.Println("E2E test completed successfully!")
}

func run() error {
	if err := buildCommands(); err != nil {
		return fmt.Errorf("failed to build commands: %w", err)
	}
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

	if err := runStatusTest(false); err != nil {
		return fmt.Errorf("post-run status test failed: %w", err)
	}

	if err := runInactiveTest(false, false); err != nil {
		return fmt.Errorf("post-run inactive test failed: %w", err)
	}

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

	if err := runStatusTest(true); err != nil {
		return fmt.Errorf("initial status test failed: %w", err)
	}

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
			"name":  "TestM3U",
			"url":   m3uURL,
			"tuner": "4",
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
	streamURLs, err := getStreamURLs(conn)
	if err != nil {
		return fmt.Errorf("failed to get stream URLs: %w", err)
	}
	for i := 1; i <= 4; i *= 2 {
		if err := runMultiStreamTest(streamURLs, i, true); err != nil {
			return fmt.Errorf("multi-stream test failed with %d streams and buffer enabled: %w", i, err)
		}
	}
	if err := runClientDisconnectTest(streamURLs[0], true); err != nil {
		return fmt.Errorf("client disconnect test failed with buffer enabled: %w", err)
	}
	if err := runRepeatedDisconnectTest(streamURLs, len(streamURLs), true); err != nil {
		return fmt.Errorf("repeated disconnect test failed with buffer enabled: %w", err)
	}
	fmt.Println("---")

	// Run with buffer disabled
	if err := setBuffer(conn, "-", 0); err != nil {
		return err
	}
	streamURLs, err = getStreamURLs(conn)
	if err != nil {
		return fmt.Errorf("failed to get stream URLs: %w", err)
	}
	for i := 1; i <= 4; i *= 2 {
		if err := runMultiStreamTest(streamURLs, i, false); err != nil {
			return fmt.Errorf("multi-stream test failed with %d streams and buffer disabled: %w", i, err)
		}
	}
	if err := runClientDisconnectTest(streamURLs[0], false); err != nil {
		return fmt.Errorf("client disconnect test failed with buffer disabled: %w", err)
	}
	if err := runRepeatedDisconnectTest(streamURLs, len(streamURLs), false); err != nil {
		return fmt.Errorf("repeated disconnect test failed with buffer disabled: %w", err)
	}

	if err := verifyTunerCountIsZero(); err != nil {
		return fmt.Errorf("tuner count verification failed: %w", err)
	}

	if err := runInactiveTest(false, true); err != nil {
		return fmt.Errorf("inactive test failed after streaming: %w", err)
	}

	return nil
}

func getActiveTunerCount() (int, error) {
	apiURL := fmt.Sprintf("http://localhost:%d/api/", xtevePort)
	requestBody := strings.NewReader(`{"cmd":"status"}`)
	resp, err := http.Post(apiURL, "application/json", requestBody)
	if err != nil {
		return 0, fmt.Errorf("failed to make status API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("status API request failed with status code: %d, body: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read status API response body: %w", err)
	}

	var responseMap map[string]interface{}
	if err := json.Unmarshal(body, &responseMap); err != nil {
		return 0, fmt.Errorf("failed to unmarshal status API response into map: %w, body: %s", err, string(body))
	}

	if tunerActive, ok := responseMap["tuners.active"]; ok {
		if ta, ok := tunerActive.(float64); ok {
			return int(ta), nil
		}
		return 0, fmt.Errorf("invalid type for tuners.active: expected float64, got %T", tunerActive)
	}

	return 0, nil // Not found, so 0
}

func verifyTunerCountIsZero() error {
	time.Sleep(2 * time.Second) // Give server a moment to clean up resources
	fmt.Println("Verifying tuner count is zero...")
	count, err := getActiveTunerCount()
	if err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("expected active tuner count to be 0, but got %d", count)
	}
	fmt.Println("Tuner count verified successfully.")
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

func runRepeatedDisconnectTest(streamURLs []string, numTuners int, buffered bool) error {
	fmt.Printf("Running repeated disconnect test with %d threads (buffered: %v)...\n", numTuners, buffered)

	if len(streamURLs) < numTuners {
		return fmt.Errorf("not enough stream URLs for repeated disconnect test, got %d, want %d", len(streamURLs), numTuners)
	}

	var wg sync.WaitGroup
	errs := make(chan error, numTuners)
	totalConnections := 100
	iterationsPerThread := totalConnections / numTuners
	extraIterations := totalConnections % numTuners

	for i := 0; i < numTuners; i++ {
		wg.Add(1)
		iterations := iterationsPerThread
		if i < extraIterations {
			iterations++
		}

		go func(threadID int, streamURL string, numIterations int) {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				if (j+1)%10 == 0 {
					fmt.Printf("[Thread %d] Connection attempt %d/%d\n", threadID, j+1, numIterations)
				}

				streamResp, err := http.Get(streamURL)
				if err != nil {
					errs <- fmt.Errorf("[Thread %d] failed to start stream on attempt %d: %w", threadID, j+1, err)
					return
				}

				// Read a small part of the body to ensure connection is established
				buffer := make([]byte, 1024)
				_, err = streamResp.Body.Read(buffer)
				// We expect an error when the stream is closed, so we only check for non-EOF errors
				if err != nil && err != io.EOF && !strings.Contains(err.Error(), "closed") {
					streamResp.Body.Close()
					errs <- fmt.Errorf("[Thread %d] failed to read from stream on attempt %d: %w", threadID, j+1, err)
					return
				}
				streamResp.Body.Close()
			}
		}(i+1, streamURLs[i], iterations)
	}

	wg.Wait()
	close(errs)

	var combinedErr error
	for err := range errs {
		if combinedErr == nil {
			combinedErr = err
		} else {
			combinedErr = fmt.Errorf("%v; %w", combinedErr, err)
		}
	}
	if combinedErr != nil {
		return combinedErr
	}

	fmt.Printf("Finished %d connect/disconnect cycles across %d threads.\n", totalConnections, numTuners)

	// Verify that tuners are zero.
	if err := verifyTunerCountIsZero(); err != nil {
		return fmt.Errorf("tuner count not zero after repeated disconnects: %w", err)
	}

	fmt.Println("Repeated disconnect test passed.")
	return nil
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

func getStreamURLs(conn *websocket.Conn) ([]string, error) {
	fmt.Println("Updating M3U file and getting stream URLs...")
	updateRequest := map[string]interface{}{"cmd": "updateFileM3U"}
	if _, err := sendRequest(conn, updateRequest); err != nil {
		return nil, fmt.Errorf("failed to send M3U update request: %w", err)
	}
	time.Sleep(5 * time.Second) // Wait for update

	httpResp, err := http.Get(fmt.Sprintf("http://localhost:%d/m3u/xteve.m3u", xtevePort))
	if err != nil {
		return nil, fmt.Errorf("failed to get M3U file: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read M3U file body: %w", err)
	}

	m3uContent := string(body)
	lines := strings.Split(m3uContent, "\n")
	var urls []string
	for i, line := range lines {
		if strings.HasPrefix(line, "#EXTINF") && strings.Contains(line, "Test Stream") {
			if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "http") {
				urls = append(urls, lines[i+1])
			}
		}
	}

	if len(urls) == 0 {
		return nil, fmt.Errorf("could not find any stream URLs in M3U output")
	}

	return urls, nil
}

func runMultiStreamTest(streamURLs []string, numStreams int, buffered bool) error {
	fmt.Printf("Running multi-stream test with %d streams (buffered: %v)...\n", numStreams, buffered)
	if len(streamURLs) < numStreams {
		return fmt.Errorf("not enough stream URLs available for the test")
	}

	var wg sync.WaitGroup
	errs := make(chan error, numStreams)

	for i := 0; i < numStreams; i++ {
		if i > 0 {
			fmt.Println("Waiting 2 seconds before starting next client...")
			time.Sleep(2 * time.Second)
		}

		wg.Add(1)
		go func(streamID int, streamURL string) {
			defer wg.Done()
			fmt.Printf("Starting stream %d from %s\n", streamID, streamURL)

			streamResp, err := http.Get(streamURL)
			if err != nil {
				errs <- fmt.Errorf("stream %d failed to start: %w", streamID, err)
				return
			}
			defer streamResp.Body.Close()

			// No inactive test needed here anymore

			receivedData, err := io.ReadAll(streamResp.Body)
			if err != nil {
				errs <- fmt.Errorf("stream %d failed to read data: %w", streamID, err)
				return
			}

			if err := verifyStreamedData(receivedData, streamID); err != nil {
				errs <- fmt.Errorf("stream %d data verification failed: %w", streamID, err)
				return
			}
			fmt.Printf("Stream %d finished and verified.\n", streamID)
		}(i+1, streamURLs[i])
	}

	var tunerCheckWg sync.WaitGroup
	if buffered {
		tunerCheckWg.Add(1)
		go func() {
			defer tunerCheckWg.Done()
			time.Sleep(5 * time.Second) // Wait for streams to be active
			activeTuners, err := getActiveTunerCount()
			if err != nil {
				errs <- fmt.Errorf("failed to get active tuner count: %w", err)
				return
			}
			if activeTuners != numStreams {
				errs <- fmt.Errorf("expected %d active tuners, but got %d", numStreams, activeTuners)
				return
			}
			fmt.Printf("Verified %d active tuners.\n", activeTuners)
		}()
	}

	wg.Wait()
	close(errs)

	if buffered {
		tunerCheckWg.Wait()
	}

	var combinedErr error
	for err := range errs {
		if combinedErr == nil {
			combinedErr = err
		} else {
			combinedErr = fmt.Errorf("%v; %w", combinedErr, err)
		}
	}

	if combinedErr != nil {
		return combinedErr
	}

	fmt.Printf("Multi-stream test with %d streams passed.\n", numStreams)
	return nil
}

func verifyStreamedData(data []byte, streamID int) error {
	expectedSize := (streamSize / mpegts.PacketSize) * mpegts.PacketSize
	if len(data) != expectedSize {
		return fmt.Errorf("streamed data size mismatch. Expected: %d, got: %d", expectedSize, len(data))
	}

	for i := 0; i < len(data); i += mpegts.PacketSize {
		packet := data[i : i+mpegts.PacketSize]
		if packet[0] != mpegts.SyncByte {
			return fmt.Errorf("invalid sync byte at offset %d", i)
		}
		for j := 1; j < mpegts.PacketSize; j++ {
			expectedByte := byte((i + j + streamID - 1) % 256)
			if packet[j] != expectedByte {
				return fmt.Errorf("streamed data content mismatch at byte %d. Expected: %d, got: %d", i+j, expectedByte, packet[j])
			}
		}
	}

	return nil
}

func buildCommands() error {
	fmt.Println("Building helper commands...")
	commands := []struct {
		Name       string
		SourcePath string
	}{
		{"xteve-status", "./cmd/xteve-status"},
		{"xteve-inactive", "./cmd/xteve-inactive"},
	}

	for _, cmdInfo := range commands {
		fmt.Printf("Building %s...\n", cmdInfo.Name)
		cmd := exec.Command("go", "build", "-o", cmdInfo.Name, cmdInfo.SourcePath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to build %s: %w", cmdInfo.Name, err)
		}
	}

	return nil
}

func runStatusTest(serverIsRunning bool) error {
	fmt.Println("Running status test...")
	cmd := exec.Command("./xteve-status", fmt.Sprintf("-port=%d", xtevePort))
	output, err := cmd.CombinedOutput()

	if serverIsRunning {
		if err != nil {
			return fmt.Errorf("xteve-status failed when server is running: %w, output: %s", err, string(output))
		}
		if !strings.Contains(string(output), "xTeVe status:") {
			return fmt.Errorf("xteve-status output did not contain expected string. got: %s", string(output))
		}
		fmt.Println("Status test passed (server running).")
	} else {
		if err == nil {
			return fmt.Errorf("xteve-status succeeded when server is not running")
		}
		if !strings.Contains(string(output), "Unable to get API") {
			return fmt.Errorf("xteve-status output did not contain expected error string. got: %s", string(output))
		}
		fmt.Println("Status test passed (server not running).")
	}

	return nil
}

func runInactiveTest(expectingActive, serverIsRunning bool) error {
	fmt.Println("Running inactive test...")
	cmd := exec.Command("./xteve-inactive", fmt.Sprintf("-port=%d", xtevePort))
	err := cmd.Run()

	if !serverIsRunning {
		if err == nil {
			return fmt.Errorf("xteve-inactive succeeded when server is not running")
		}
		fmt.Println("Inactive test passed (server not running).")
		return nil
	}

	exitCode := cmd.ProcessState.ExitCode()
	if expectingActive {
		if exitCode == 0 {
			return fmt.Errorf("xteve-inactive returned exit code 0 when expecting active stream")
		}
		fmt.Printf("Inactive test passed (expecting active), exit code: %d\n", exitCode)
	} else {
		if exitCode != 0 {
			return fmt.Errorf("xteve-inactive returned exit code %d when expecting no active stream", exitCode)
		}
		fmt.Println("Inactive test passed (expecting inactive).")
	}

	return nil
}
