package authentication

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserAuthentication_Security(t *testing.T) {
	// Setup temporary database
	tmpDir, err := os.MkdirTemp("", "auth_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbFile := filepath.Join(tmpDir, "authentication.json")

	// Initialize
	err = Init(dbFile, 60)
	if err != nil {
		t.Fatal(err)
	}

	// Create a user
	username := "testuser"
	password := "securepassword123"
	_, err = CreateNewUser(username, password)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// 1. Test Valid Login
	token, err := UserAuthentication(username, password)
	if err != nil {
		t.Errorf("Valid login failed: %v", err)
	}
	if token == "" {
		t.Error("Valid login returned empty token")
	}

	// 2. Test Invalid Password
	_, err = UserAuthentication(username, "wrongpassword")
	if err == nil {
		t.Error("Invalid password should return error, got nil")
	} else if err.Error() != "User authentication failed" {
		t.Errorf("Expected 'User authentication failed', got '%v'", err)
	}

	// 3. Test Invalid Username
	_, err = UserAuthentication("wronguser", password)
	if err == nil {
		t.Error("Invalid username should return error, got nil")
	} else if err.Error() != "User authentication failed" {
		t.Errorf("Expected 'User authentication failed', got '%v'", err)
	}

	// 4. Test Case Sensitivity (Username)
	// Assuming usernames are case sensitive based on SHA256 hashing
	_, err = UserAuthentication("TestUser", password)
	if err == nil {
		t.Error("Case mismatched username should return error (assuming case sensitivity), got nil")
	}

	// 5. Test Case Sensitivity (Password)
	_, err = UserAuthentication(username, "SecurePassword123")
	if err == nil {
		t.Error("Case mismatched password should return error, got nil")
	}
}
