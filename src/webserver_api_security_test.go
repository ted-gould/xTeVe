package src

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"xteve/src/internal/authentication"
)

func TestAPI_AuthBypass_ReverseProxy(t *testing.T) {
	// 1. Setup Environment
	tempDir, err := os.MkdirTemp("", "xteve_test_api_security")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configDir := filepath.Join(tempDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	// Save/Restore Globals
	originalConfigFolder := System.Folder.Config
	originalAuthWeb := Settings.AuthenticationWEB
	originalAuthAPI := Settings.AuthenticationAPI
	defer func() {
		System.Folder.Config = originalConfigFolder
		Settings.AuthenticationWEB = originalAuthWeb
		Settings.AuthenticationAPI = originalAuthAPI
	}()

	System.Folder.Config = configDir + string(os.PathSeparator)

	// Set the Vulnerable Configuration:
	// Web Auth ENABLED (User thinks they are secure)
	// API Auth DISABLED (Default, User forgot or didn't know)
	Settings.AuthenticationWEB = true
	Settings.AuthenticationAPI = false

	// 2. Initialize Authentication
	// Create database file path
	dbPath := filepath.Join(configDir, "authentication.json")
	if err := authentication.Init(dbPath, 60); err != nil {
		t.Fatalf("Failed to init auth: %v", err)
	}

	username := "admin"
	password := "admin"
	if err := authentication.CreateDefaultUser(username, password); err != nil {
		// Ignore if already exists (shouldn't happen with unique temp dir)
		t.Logf("CreateDefaultUser note: %v", err)
	}

	// Create User with permissions
	// (CreateDefaultUser creates user, but permissions are stored in 'data'.
	// authentication.CreateDefaultUser doesn't expose permission setting directly.
	// But API handler checks authentication via UserAuthentication which checks username/password.
	// It doesn't strictly check 'authentication.api' permission for login?
	// Wait, API handler checks `checkAuthorizationLevel(token, "authentication.api")` IF auth is enforced.
	// If auth is NOT enforced (Settings.AuthenticationAPI = false), it bypasses everything.
	// If we FIX it, it will enforce auth. And then it will check permission.
	// So we need to ensure the user has 'authentication.api' permission if we want them to succeed when authorized.
	// But for this test, we want to prove that WITHOUT token, access is denied.

	// 3. Test Case: Unauthenticated Request to API
	// We send a command "status" which should return system info.
	requestBody, _ := json.Marshal(map[string]string{
		"cmd": "status",
	})

	req := httptest.NewRequest("POST", "/api/", bytes.NewBuffer(requestBody))
	req.RemoteAddr = "127.0.0.1:12345" // Simulate Localhost / Reverse Proxy
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	// Call API handler
	API(w, req)

	// 4. Analyze Response
	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("API returned status %d", resp.StatusCode)
	}

	var apiResp APIResponseStruct
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		t.Fatalf("Failed to decode JSON response: %v", err)
	}

	// BEFORE FIX:
	// Settings.AuthenticationAPI = false.
	// API allows localhost.
	// So status should be TRUE.
	// This confirms the vulnerability: Web Auth is ON, but API is OPEN.

	// AFTER FIX:
	// We expect status to be FALSE or Error to be set, because we provided NO token.

	if apiResp.Status {
		t.Errorf("VULNERABILITY CONFIRMED: API access allowed without authentication while Web Auth is enabled! Status: %v", apiResp.Status)
	} else {
		// If status is false, verify it's an auth error
		if apiResp.Error != "login incorrect" && apiResp.Error != "no authorization" {
			// "login incorrect" is what it returns if no token provided and not login command
			t.Logf("API Access Denied as expected. Error: %s", apiResp.Error)
		}
	}
}
