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

// Request and Response structs based on frontend code
type WebSocketRequest struct {
	Cmd  string      `json:"cmd"`
	Data interface{} `json:"data,omitempty"`
}

type M3UFile struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Category string `json:"category,omitempty"`
}

type Filter struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Filter      string `json:"filter"`
	Case        string `json:"case"`
	Active      bool   `json:"active"`
	Rule        string `json:"rule"`
	Description string `json:"description"`
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
	buildCmd.Dir = "../../" // Run from the root directory
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to build xteve: %w\n%s", err, string(buildOutput))
	}

	// Remove existing config to ensure a clean slate
	os.RemoveAll("../../.xteve")

	cmd := exec.Command("./xteve_test_binary", "-port=34400")
	cmd.Dir = "../../" // Run from the root directory
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

func runTests() error {
	fmt.Println("Running tests...")

	// 1. Connect to the WebSocket
	wsURL := "ws://localhost:34400/data/"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to websocket: %w", err)
	}
	defer conn.Close()

	// 2. Add an M3U playlist
	fmt.Println("Adding M3U playlist...")
	m3uData := map[string]interface{}{
		"0": map[string]string{
			"name": "CSPAN",
			"url":  "https://raw.githubusercontent.com/freearhey/iptv-usa/main/c-span.us.m3u",
		},
	}
	m3uRequest := map[string]interface{}{
		"cmd":  "saveFilesM3U",
		"data": m3uData,
	}
	if err := conn.WriteJSON(m3uRequest); err != nil {
		return fmt.Errorf("failed to send M3U save request: %w", err)
	}
	// Read response to confirm
	_, _, err = conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read M3U save response: %w", err)
	}

	// 3. Add a filter
	fmt.Println("Adding filter...")
	filterData := map[string]interface{}{
		"0": map[string]interface{}{
			"active":      true,
			"case":        "sensitive",
			"description": "Filter out CSPAN 2",
			"exclude":     "",
			"filter":      "CSPAN 2",
			"include":     "",
			"name":        "CSPAN2-Filter",
			"type":        "group-title",
		},
	}

	filterRequest := map[string]interface{}{
		"cmd":  "saveFilter",
		"data": filterData,
	}

	if err := conn.WriteJSON(filterRequest); err != nil {
		return fmt.Errorf("failed to send filter save request: %w", err)
	}
	// Read response to confirm
	_, _, err = conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read filter save response: %w", err)
	}

	// 4. Verify the M3U output
	fmt.Println("Verifying M3U output...")
	// Need to trigger an update for the filter to apply
	updateRequest := map[string]interface{}{"cmd": "update.m3u"}
	if err := conn.WriteJSON(updateRequest); err != nil {
		return fmt.Errorf("failed to send M3U update request: %w", err)
	}
	_, _, err = conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read M3U update response: %w", err)
	}

	// Wait for the update to process
	time.Sleep(5 * time.Second)

	resp, err := http.Get("http://localhost:34400/m3u/xteve.m3u")
	if err != nil {
		return fmt.Errorf("failed to get M3U file: %w", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
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
