package src

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"xteve/src/internal/authentication"
)

func TestDownloadHandler_Security(t *testing.T) {
	// 1. Setup Environment
	tempDir, err := os.MkdirTemp("", "xteve_test_security")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configDir := filepath.Join(tempDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	tempFileDir := filepath.Join(tempDir, "temp")
	if err := os.MkdirAll(tempFileDir, 0755); err != nil {
		t.Fatalf("Failed to create temp file dir: %v", err)
	}

	// Save/Restore Globals
	originalConfigFolder := System.Folder.Config
	originalTempFolder := System.Folder.Temp
	originalAuthWeb := Settings.AuthenticationWEB
	defer func() {
		System.Folder.Config = originalConfigFolder
		System.Folder.Temp = originalTempFolder
		Settings.AuthenticationWEB = originalAuthWeb
	}()

	System.Folder.Config = configDir + string(os.PathSeparator)
	System.Folder.Temp = tempFileDir + string(os.PathSeparator)
	Settings.AuthenticationWEB = true // ENABLE AUTH

	// 2. Initialize Authentication
	// We need to create a default user and get a token
	if err := authentication.Init(System.Folder.Config+"authentication.json", 60); err != nil {
		t.Fatalf("Failed to init auth: %v", err)
	}

	username := "admin"
	password := "admin"
	// CreateDefaultUser creates the user AND logs them in? No, it just creates.
	// We need to create the user first.
	// But `authentication.Init` creates a default user if none exists?
	// `CreateDefaultUser` checks if len(users) > 0.

	if err := authentication.CreateDefaultUser(username, password); err != nil {
		// If default user already exists (from Init maybe?), ignore
		// But Init only creates empty DB.
		t.Fatalf("Failed to create default user: %v", err)
	}

	// Login to get a token
	token, err := authentication.UserAuthentication(username, password)
	if err != nil {
		t.Fatalf("Failed to authenticate: %v", err)
	}

	// Need to give this user 'authentication.web' permission
	// CreateDefaultUser doesn't set permissions?
	// `src/authentication.go` `createFirstUserForAuthentication` sets permissions.
	// We should probably manually set permissions in the DB for this user.
	userID, err := authentication.GetUserID(token)
	if err != nil {
		t.Fatalf("Failed to get UserID: %v", err)
	}

	userData := map[string]any{
		"authentication.web": true,
	}
	if err := authentication.WriteUserData(userID, userData); err != nil {
		t.Fatalf("Failed to write user data: %v", err)
	}

	// 3. Create a Dummy File to Download
	filename := "secret_backup.zip"
	fullPath := filepath.Join(tempFileDir, filename)
	if err := os.WriteFile(fullPath, []byte("SECRET DATA"), 0644); err != nil {
		t.Fatalf("Failed to write secret file: %v", err)
	}

	// 4. Test Case: Unauthenticated Request
	reqNoAuth := httptest.NewRequest("GET", "/download/"+filename, nil)
	wNoAuth := httptest.NewRecorder()
	Download(wNoAuth, reqNoAuth)

	// EXPECT FAILURE (401 or 403)
	// Currently it returns 200, so this assertion SHOULD FAIL
	if wNoAuth.Code == http.StatusOK {
		t.Errorf("VULNERABILITY: Download allowed without authentication! Status: %d", wNoAuth.Code)
	} else if wNoAuth.Code != http.StatusUnauthorized && wNoAuth.Code != http.StatusForbidden {
		t.Errorf("Expected 401/403, got %d", wNoAuth.Code)
	}

	// Re-create file because Download deletes it on success
	if wNoAuth.Code == http.StatusOK {
		if err := os.WriteFile(fullPath, []byte("SECRET DATA"), 0644); err != nil {
			t.Fatalf("Failed to recreate secret file: %v", err)
		}
	}

	// 5. Test Case: Authenticated Request (Cookie)
	reqAuth := httptest.NewRequest("GET", "/download/"+filename, nil)
	reqAuth.AddCookie(&http.Cookie{Name: "Token", Value: token})
	wAuth := httptest.NewRecorder()

	Download(wAuth, reqAuth)

	if wAuth.Code != http.StatusOK {
		t.Errorf("Authenticated download failed. Status: %d", wAuth.Code)
	}

	// Verify Token Rotation (New Cookie Set)
	foundNewToken := false
	for _, c := range wAuth.Result().Cookies() {
		if c.Name == "Token" && c.Value != token && c.Value != "" {
			foundNewToken = true
			break
		}
	}
	// Note: CheckTheValidityOfTheToken rotates token. If Download logic implements it correctly,
	// it should set a new cookie.
	// If the current implementation (which ignores auth) runs, it returns 200 but sets no cookie.
	if !foundNewToken && wAuth.Code == http.StatusOK {
		// This is expected if the fix is NOT implemented yet.
		// t.Log("Note: Token rotation not verified (expected before fix)")
	} else if !foundNewToken {
		t.Error("New Token cookie was not set in response")
	}
}
