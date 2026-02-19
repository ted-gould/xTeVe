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
		if _, err := w.Write([]byte(content)); err != nil {
			log.Printf("mock server write error: %v", err)
		}
	})
	mux.HandleFunc("/remote-time/", func(w http.ResponseWriter, r *http.Request) {
		// Content-Length AND Last-Modified
		content := "video content"
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Header().Set("Last-Modified", RemoteTime.Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(content)); err != nil {
			log.Printf("mock server write error: %v", err)
		}
	})

	addr := fmt.Sprintf(":%d", MockServerPort)
	fmt.Printf("Starting mock server on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Mock server failed: %v", err)
	}
}

func createM3UFile() (string, error) {
	// Use 127.0.0.1 explicitly to avoid localhost resolution issues in some environments
	// xTeVe might not like 'localhost' if strict parsing is used somewhere
	content := fmt.Sprintf(`#EXTM3U

#EXTINF:0 time="%s" size="1000" group-title="TestGroup",Internal Time
http://127.0.0.1:%d/no-time/internal.mp4

#EXTINF:0 size="2000" group-title="TestGroup",Remote Time
http://127.0.0.1:%d/remote-time/remote.mp4

#EXTINF:0 size="3000" group-title="TestGroup",Fallback Time
http://127.0.0.1:%d/no-time/fallback.mp4
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
	// Set XTEVE_ALLOW_LOOPBACK to true to bypass SSRF protection for test mock server
	cmd.Env = append(os.Environ(), "XTEVE_ALLOW_LOOPBACK=true")
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
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("failed to kill xteve process: %v", err)
		}
	}
	if err := exec.Command("pkill", "xteve_ts_binary").Run(); err != nil {
		// Ignore error if process not found
		_ = err
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
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("failed to set read deadline: %w", err)
	}
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
	if err := exec.Command("fusermount", "-u", MountPoint).Run(); err != nil {
		log.Printf("fusermount error (ignorable): %v", err)
	}
	if err := exec.Command("umount", MountPoint).Run(); err != nil {
		log.Printf("umount error (ignorable): %v", err)
	}
}

func verifyFiles() error {
	fmt.Println("Verifying files with retry...")

	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			debugListDir(MountPoint)
			return fmt.Errorf("timeout waiting for files verification")
		case <-ticker.C:
			if err := checkFiles(); err == nil {
				return nil
			}
		}
	}
}

func debugListDir(root string) {
	fmt.Println("DEBUG: Listing mounted directory:")
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Error accessing %s: %v\n", path, err)
			return nil
		}
		fmt.Printf("%s (IsDir: %v, Size: %d, ModTime: %s)\n", path, info.IsDir(), info.Size(), info.ModTime())
		return nil
	}); err != nil {
		fmt.Printf("Walk error: %v\n", err)
	}
}

func checkFiles() error {
	// Find hash directory
	entries, err := os.ReadDir(MountPoint)
	if err != nil {
		return err
	}

	var hashDir string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "On Demand" {
			hashDir = e.Name()
			break
		}
	}

	if hashDir == "" {
		return fmt.Errorf("could not find hash directory in root")
	}

	// Path structure: /<hash>/On Demand/TestGroup/Individual/
	groupDir := filepath.Join(MountPoint, hashDir, "On Demand", "TestGroup", "Individual")
	entries, err = os.ReadDir(groupDir)
	if err != nil {
		// Try without "Individual" if grouping is different, but for M3U it defaults to Individual if not Series
		// Let's list the TestGroup dir to be safe
		groupDir = filepath.Join(MountPoint, hashDir, "On Demand", "TestGroup")
		entries, err = os.ReadDir(groupDir)
		if err != nil {
			return fmt.Errorf("failed to read group dir %s: %w", groupDir, err)
		}

		// If "Individual" exists, traverse into it
		for _, e := range entries {
			if e.IsDir() && e.Name() == "Individual" {
				groupDir = filepath.Join(groupDir, "Individual")
				entries, _ = os.ReadDir(groupDir)
				break
			}
		}
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

	// Internal Time: Should be correct (2021-02-02)
	if err := verify("Internal Time.mp4", InternalTime, 2*time.Second); err != nil {
		return err
	}

	// Remote Time: Should be correct (2022-03-03)
	if err := verify("Remote Time.mp4", RemoteTime, 2*time.Second); err != nil {
		return err
	}

	// Fallback Time:
	// xTeVe copies the M3U to its data directory, so the modification time will be "Now".
	// We can't check against M3UFileTime (2020) because the file in data/ is new.
	// So we check if it is recent (within 1 minute).

	sanitizedFallback := strings.ReplaceAll("Fallback Time.mp4", " ", "_")
	fallbackTime, ok := found[sanitizedFallback]
	if !ok {
		fallbackTime, ok = found["Fallback Time.mp4"]
	}
	if !ok {
		return fmt.Errorf("file Fallback Time.mp4 not found")
	}

	if time.Since(fallbackTime) > 1*time.Minute {
		return fmt.Errorf("file Fallback Time.mp4 timestamp mismatch: expected recent (now), got %s", fallbackTime)
	}

	return nil
}
