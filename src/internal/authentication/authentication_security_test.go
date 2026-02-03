package authentication

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserAuthentication_SecurityRegression(t *testing.T) {
	// Setup temporary directory
	tmpDir, err := os.MkdirTemp("", "auth_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Init takes a file path but uses its directory to store authentication.json
	dbPath := filepath.Join(tmpDir, "dummy")

	// Initialize authentication
	err = Init(dbPath, 60)
	if err != nil {
		t.Fatal(err)
	}

	// Create a user
	username := "testuser"
	password := "testpass"
	userID, err := CreateNewUser(username, password)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	if userID == "" {
		t.Fatal("Expected userID, got empty string")
	}

	// Test Valid Login
	token, err := UserAuthentication(username, password)
	if err != nil {
		t.Errorf("Valid login failed: %v", err)
	}
	if token == "" {
		t.Error("Expected token, got empty string")
	}

	// Test Invalid Password
	_, err = UserAuthentication(username, "wrongpass")
	if err == nil {
		t.Error("Expected error for invalid password, got nil")
	}

	// Test Invalid Username
	_, err = UserAuthentication("wronguser", password)
	if err == nil {
		t.Error("Expected error for invalid username, got nil")
	}
}
