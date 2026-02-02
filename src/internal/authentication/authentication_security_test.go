package authentication

import (
	"path/filepath"
	"testing"
)

func TestUserAuthentication_Security(t *testing.T) {
	// Setup temporary database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "authentication_test.json")

	// Initialize authentication
	err := Init(dbPath, 60)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create a user
	username := "admin"
	password := "securepassword123"
	userID, err := CreateNewUser(username, password)
	if err != nil {
		t.Fatalf("CreateNewUser failed: %v", err)
	}
	if userID == "" {
		t.Fatal("CreateNewUser returned empty userID")
	}

	// Test correct credentials
	token, err := UserAuthentication(username, password)
	if err != nil {
		t.Errorf("Authentication failed with correct credentials: %v", err)
	}
	if token == "" {
		t.Error("Token is empty after successful authentication")
	}

	// Test incorrect password
	token, err = UserAuthentication(username, "wrongpassword")
	if err == nil {
		t.Error("Authentication succeeded with wrong password")
	}
	if token != "" {
		t.Errorf("Expected empty token for wrong password, got %s", token)
	}

	// Test incorrect username
	token, err = UserAuthentication("wronguser", password)
	if err == nil {
		t.Error("Authentication succeeded with wrong username")
	}
	if token != "" {
		t.Errorf("Expected empty token for wrong username, got %s", token)
	}
}
