package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketResponse defines the structure of a response from the server.
type WebSocketResponse struct {
	Status bool   `json:"status"`
	Error  string `json:"err,omitempty"`
}

func main() {
	// 1. Start the xteve server
	cmd, err := startXteve()
	if err != nil {
		log.Fatalf("Failed to start xteve: %v", err)
	}
	defer stopXteve(cmd)

	// Wait for the server to be ready
	if err := waitForServerReady("http://localhost:34400/web/"); err != nil {
		log.Fatalf("Server not ready: %v", err)
	}

	// 2. Run the tests
	if err := runTests(); err != nil {
		log.Fatalf("Tests failed: %v", err)
	}

	fmt.Println("CI test completed successfully!")
}

func startXteve() (*exec.Cmd, error) {
	fmt.Println("Starting xteve server...")
	// Build the xteve binary first
	buildCmd := exec.Command("go", "build", "-o", "xteve_test_binary", "xteve.go")
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to build xteve: %w\n%s", err, string(buildOutput))
	}

	// Remove existing config to ensure a clean slate
	os.RemoveAll(".xteve")

	cmd := exec.Command("./xteve_test_binary", "-port=34400")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func stopXteve(cmd *exec.Cmd) {
	fmt.Println("Stopping xteve server...")
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

func runTests() error {
	fmt.Println("Running tests...")

	// 1. Connect to the WebSocket
	wsURL := "ws://localhost:34400/data/"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to websocket: %w", err)
	}
	defer conn.Close()

	// 2. Test: Add an invalid filter (should fail gracefully)
	fmt.Println("Testing invalid filter...")
	invalidFilterData := map[string]interface{}{
		"0": map[string]interface{}{
			"type": "group-title", // Missing "name"
		},
	}
	invalidFilterRequest := map[string]interface{}{
		"cmd":  "saveFilter",
		"data": invalidFilterData,
	}

	resp, err := sendRequest(conn, invalidFilterRequest)
	if err != nil {
		return fmt.Errorf("error testing invalid filter: %w", err)
	}
	if resp.Status {
		return fmt.Errorf("expected status false for invalid filter, but got true")
	}
	if !strings.Contains(resp.Error, "filter 'name' is a required field") {
		return fmt.Errorf("expected error message for invalid filter, but got: %s", resp.Error)
	}
	fmt.Println("Server correctly handled invalid filter.")

	// 3. Test: Add a valid M3U playlist
	fmt.Println("Adding M3U playlist...")
	m3uData := map[string]interface{}{
		"0": map[string]string{
			"name": "CSPAN",
			"url":  "https://raw.githubusercontent.com/freearhey/iptv-usa/main/c-span.us.m3u",
		},
	}
	m3uRequest := map[string]interface{}{"cmd": "saveFilesM3U", "data": m3uData}
	if _, err := sendRequest(conn, m3uRequest); err != nil {
		return fmt.Errorf("failed to add M3U playlist: %w", err)
	}

	// 4. Test: Add a valid filter
	fmt.Println("Adding valid filter...")
	validFilterData := map[string]interface{}{
		"0": map[string]interface{}{
			"active":          true,
			"caseSensitive":   true,
			"description":     "Filter out CSPAN 2",
			"exclude":         "",
			"filter":          "CSPAN 2",
			"include":         "",
			"name":            "CSPAN2-Filter",
			"type":            "group-title",
			"preserveMapping": true,
			"rule":            "",
			"startingChannel": 1000,
		},
	}
	validFilterRequest := map[string]interface{}{"cmd": "saveFilter", "data": validFilterData}
	if _, err := sendRequest(conn, validFilterRequest); err != nil {
		return fmt.Errorf("failed to add valid filter: %w", err)
	}

	// 5. Test: Verify the M3U output
	fmt.Println("Verifying M3U output...")
	updateRequest := map[string]interface{}{"cmd": "update.m3u"}
	if _, err := sendRequest(conn, updateRequest); err != nil {
		return fmt.Errorf("failed to send M3U update request: %w", err)
	}

	// Wait for the update to process
	time.Sleep(5 * time.Second)

	httpResp, err := http.Get("http://localhost:34400/m3u/xteve.m3u")
	if err != nil {
		return fmt.Errorf("failed to get M3U file: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := ioutil.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read M3U file body: %w", err)
	}

	m3uContent := string(body)
	if strings.Contains(m3uContent, "CSPAN 2") {
		return fmt.Errorf("filter failed: 'CSPAN 2' found in M3U output")
	}
	if !strings.Contains(m3uContent, "CSPAN") {
		return fmt.Errorf("verification failed: 'CSPAN' not found in M3U output")
	}

	fmt.Println("Filter successfully applied.")
	return nil
}
