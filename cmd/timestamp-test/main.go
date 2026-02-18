package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Constants for expected timestamps
var (
	M3UFileTime      = time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	InternalTime     = time.Date(2021, 2, 2, 12, 0, 0, 0, time.UTC)
	RemoteTime       = time.Date(2022, 3, 3, 12, 0, 0, 0, time.UTC)
	MockServerPort   = 34401
	XTeVePort        = 34400
	MountPoint       = "./mnt_xteve"
	ConfigDir        = ".xteve_ts_test"
	RcloneConfigFile = "rclone.conf"
)

type WebSocketResponse struct {
	Status bool   `json:"status"`
	Error  string `json:"err,omitempty"`
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("Test failed: %v", err)
	}
	fmt.Println("Test passed!")
}

func run() error {
	// Cleanup any previous run artifacts
	cleanup()
	defer cleanup()

	// 1. Start Mock Server
	go startMockServer()

	// Allow server to start
	time.Sleep(1 * time.Second)

	// 2. Prepare M3U File
	m3uPath, err := createM3UFile()
	if err != nil {
		return fmt.Errorf("failed to create M3U: %w", err)
	}
	defer os.Remove(m3uPath)

	// 3. Start xTeVe
	cmd, err := startXteve()
	if err != nil {
		return fmt.Errorf("failed to start xteve: %w", err)
	}
	defer stopXteve(cmd)

	if err := waitForServerReady(fmt.Sprintf("http://localhost:%d/web/", XTeVePort)); err != nil {
		return fmt.Errorf("xteve not ready: %w", err)
	}

	// 4. Configure xTeVe via WebSocket
	if err := configureXteve(m3uPath); err != nil {
		return fmt.Errorf("failed to configure xteve: %w", err)
	}

	// 5. Mount with Rclone
	if err := mountRclone(); err != nil {
		return fmt.Errorf("failed to mount rclone: %w", err)
	}
	defer unmountRclone()

	// 6. Verify Files
	if err := verifyFiles(); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	return nil
}

func cleanup() {
	stopXteve(nil) // Just in case, though handled by run()
	os.RemoveAll(ConfigDir)
	unmountRclone()
	os.RemoveAll(MountPoint)
	os.Remove(RcloneConfigFile)
}

func startMockServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/no-time/", func(w http.ResponseWriter, r *http.Request) {
		// Only Content-Length, no Last-Modified
		content := "video content"
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	})
	mux.HandleFunc("/remote-time/", func(w http.ResponseWriter, r *http.Request) {
		// Content-Length AND Last-Modified
		content := "video content"
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Header().Set("Last-Modified", RemoteTime.Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	})

	addr := fmt.Sprintf(":%d", MockServerPort)
	fmt.Printf("Starting mock server on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Mock server failed: %v", err)
	}
}

func createM3UFile() (string, error) {
	content := fmt.Sprintf(`#EXTM3U

#EXTINF:0 time="%s" size="1000" group-title="TestGroup",Internal Time
http://localhost:%d/no-time/internal.mp4

#EXTINF:0 size="2000" group-title="TestGroup",Remote Time
http://localhost:%d/remote-time/remote.mp4

#EXTINF:0 size="3000" group-title="TestGroup",Fallback Time
http://localhost:%d/no-time/fallback.mp4
`, InternalTime.Format(time.RFC3339), MockServerPort, MockServerPort, MockServerPort)

	cwd, _ := os.Getwd()
	path := filepath.Join(cwd, "test.m3u")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}

	// Set modification time for fallback check
	if err := os.Chtimes(path, time.Now(), M3UFileTime); err != nil {
		return "", err
	}

	return path, nil
}

func startXteve() (*exec.Cmd, error) {
	fmt.Println("Starting xteve server...")
	// Build the xteve binary first. Using "." to build the package in current directory.
	buildCmd := exec.Command("go", "build", "-o", "xteve_ts_binary", ".")
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to build xteve: %w\n%s", err, string(buildOutput))
	}

	cmd := exec.Command("./xteve_ts_binary", fmt.Sprintf("-port=%d", XTeVePort), fmt.Sprintf("-config=%s", ConfigDir))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func stopXteve(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		fmt.Println("Stopping xteve server...")
		cmd.Process.Kill()
	}
	// Ensure process named xteve_ts_binary is killed
	exec.Command("pkill", "xteve_ts_binary").Run()
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

func configureXteve(m3uPath string) error {
	fmt.Println("Configuring xteve...")
	wsURL := fmt.Sprintf("ws://localhost:%d/data/", XTeVePort)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to websocket: %w", err)
	}
	defer conn.Close()

	// Add M3U Playlist
	m3uData := map[string]interface{}{
		"-": map[string]interface{}{
			"name": "TestPlaylist",
			"url":  m3uPath,
		},
	}
	m3uRequest := map[string]interface{}{"cmd": "saveFilesM3U", "files": map[string]interface{}{"m3u": m3uData}}
	if err := sendRequest(conn, m3uRequest); err != nil {
		return fmt.Errorf("failed to add M3U playlist: %w", err)
	}

	// Trigger update
	fmt.Println("Triggering M3U update...")
	updateRequest := map[string]interface{}{"cmd": "updateFileM3U"}
	if err := sendRequest(conn, updateRequest); err != nil {
		return fmt.Errorf("failed to trigger update: %w", err)
	}

	// Wait a bit for processing before WebDAV access
	time.Sleep(2 * time.Second)

	return nil
}

func sendRequest(conn *websocket.Conn, request map[string]interface{}) error {
	if err := conn.WriteJSON(request); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	// We expect a response, usually status: true
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var response WebSocketResponse
	if err := json.Unmarshal(msg, &response); err != nil {
		fmt.Printf("Warning: received non-standard response: %s\n", string(msg))
	} else if !response.Status {
		return fmt.Errorf("server returned error: %s", response.Error)
	}
	return nil
}

func createRcloneConfig() error {
	content := fmt.Sprintf(`[xteve]
type = webdav
url = http://localhost:%d/dav/
vendor = other
`, XTeVePort)
	return os.WriteFile(RcloneConfigFile, []byte(content), 0644)
}

func mountRclone() error {
	if err := os.MkdirAll(MountPoint, 0755); err != nil {
		return err
	}

	if err := createRcloneConfig(); err != nil {
		return fmt.Errorf("failed to create rclone config: %w", err)
	}

	fmt.Println("Mounting WebDAV via rclone...")

	args := []string{
		"mount",
		"xteve:",
		MountPoint,
		"--config", RcloneConfigFile,
		"--vfs-cache-mode", "off",
		// "--read-only", // Optional, but WebDAV here is effectively read-only
	}

	cmd := exec.Command("rclone", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	// Wait for mount to appear
	fmt.Println("Waiting for mount...")
	for i := 0; i < 10; i++ {
		if isMounted(MountPoint) {
			fmt.Println("Mount is ready.")
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("mount failed to appear")
}

func isMounted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = f.Readdirnames(1)
	return err == nil || err == io.EOF
}

func unmountRclone() {
	fmt.Println("Unmounting...")
	exec.Command("fusermount", "-u", MountPoint).Run()
	exec.Command("umount", MountPoint).Run()
}

func verifyFiles() error {
	fmt.Println("Verifying files with retry...")

	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			// Debug: List directory contents on failure
			debugListDir(MountPoint)
			return fmt.Errorf("timeout waiting for files verification")
		case <-ticker.C:
			if err := checkFiles(); err == nil {
				return nil
			} else {
				// Keep trying, maybe log failure but not return
				// fmt.Printf("Check failed, retrying... (%v)\n", err)
			}
		}
	}
}

func debugListDir(root string) {
	fmt.Println("DEBUG: Listing mounted directory:")
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Error accessing %s: %v\n", path, err)
			return nil
		}
		fmt.Printf("%s (IsDir: %v, Size: %d, ModTime: %s)\n", path, info.IsDir(), info.Size(), info.ModTime())
		return nil
	})
}

func checkFiles() error {
	// Expected paths
	// Root will contain the hash of the M3U file
	// Then On Demand, then TestGroup...

	// We need to find the hash directory first
	entries, err := os.ReadDir(MountPoint)
	if err != nil {
		return err
	}

	var hashDir string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "On Demand" { // Assuming hash is not "On Demand"
			// Actually, "On Demand" is inside the hash directory in WebDAV implementation?
			// WebDAVFS OpenFile:
			// parts[0] is hash
			// parts[1] is "On Demand"
			// So structure is /<hash>/On Demand/<Group>/...
			hashDir = e.Name()
			break
		}
	}

	if hashDir == "" {
		return fmt.Errorf("could not find hash directory in root")
	}

	groupDir := filepath.Join(MountPoint, hashDir, "On Demand", "TestGroup")
	entries, err = os.ReadDir(groupDir)
	if err != nil {
		return fmt.Errorf("failed to read group dir %s: %w", groupDir, err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("no files found yet")
	}

	found := make(map[string]time.Time)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		found[entry.Name()] = info.ModTime()
	}

	verify := func(filename string, expected time.Time, tolerance time.Duration) error {
		sanitized := strings.ReplaceAll(filename, " ", "_")

		actual, ok := found[sanitized]
		if !ok {
			actual, ok = found[filename]
			if !ok {
				return fmt.Errorf("file %s not found", filename)
			}
		}

		diff := actual.Sub(expected)
		if diff < 0 {
			diff = -diff
		}
		if diff > tolerance {
			return fmt.Errorf("file %s timestamp mismatch: expected %s, got %s (diff %s)", filename, expected, actual, diff)
		}
		return nil
	}

	if err := verify("Internal Time.mp4", InternalTime, 2*time.Second); err != nil {
		return err
	}

	if err := verify("Remote Time.mp4", RemoteTime, 2*time.Second); err != nil {
		return err
	}

	if err := verify("Fallback Time.mp4", M3UFileTime, 2*time.Second); err != nil {
		return err
	}

	return nil
}
