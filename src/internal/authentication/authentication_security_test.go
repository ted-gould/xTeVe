package authentication

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSecurity_PasswordHashing(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "auth_security_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "config")
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize
	if err := Init(filepath.Join(dbPath, "dummy"), 60); err != nil {
		t.Fatal(err)
	}

	username := "secureuser"
	password := "securepassword"

	// Create User
	userID, err := CreateNewUser(username, password)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Verify Data in Memory (internal 'data' map)
	mu.RLock()
	users := data["users"].(map[string]any)
	userData := users[userID].(map[string]any)
	mu.RUnlock()

	storedPassword := userData["_password"].(string)
	storedSalt := userData["_salt"].(string)

	t.Logf("Stored Password Hash: %s", storedPassword)
	t.Logf("Stored Salt: %s", storedSalt)

	// Verify it IS bcrypt
	if strings.HasPrefix(storedPassword, "$2a$") {
		t.Log("Password IS bcrypt encoded (SUCCESS)")
	} else {
		t.Error("Password is NOT bcrypt encoded!")
	}

	// Verify Authentication Works
	token, err := UserAuthentication(username, password)
	if err != nil {
		t.Errorf("Authentication failed with correct password: %v", err)
	}
	if token == "" {
		t.Error("Token is empty")
	}

	// Verify Authentication Fails with wrong password
	_, err = UserAuthentication(username, "wrongpassword")
	if err == nil {
		t.Error("Authentication succeeded with wrong password!")
	}
}

func TestSecurity_Migration(t *testing.T) {
	// Setup
	tempDir, err := os.MkdirTemp("", "auth_migration_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "config")
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Initialize
	if err := Init(filepath.Join(dbPath, "dummy"), 60); err != nil {
		t.Fatal(err)
	}

	username := "legacyuser"
	password := "legacypassword"

	// Create User (which will use bcrypt now)
	userID, err := CreateNewUser(username, password)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Manually degrade to HMAC-SHA256
	mu.Lock()
	users := data["users"].(map[string]any)
	userData := users[userID].(map[string]any)
	salt := userData["_salt"].(string)

	legacyHash, err := SHA256(password, salt)
	if err != nil {
		t.Fatal(err)
	}
	userData["_password"] = legacyHash
	// Ensure username is also hashed as HMAC-SHA256 (CreateNewUser already does this via defaultsForNewUser, and we kept that)
	mu.Unlock()

	t.Log("Manually degraded user password to HMAC-SHA256")

	// Login
	token, err := UserAuthentication(username, password)
	if err != nil {
		t.Fatalf("Authentication failed for legacy user: %v", err)
	}
	if token == "" {
		t.Error("Token is empty")
	}

	// Verify Upgraded to bcrypt
	mu.RLock()
	users = data["users"].(map[string]any)
	userData = users[userID].(map[string]any)
	newStoredPassword := userData["_password"].(string)
	mu.RUnlock()

	if strings.HasPrefix(newStoredPassword, "$2a$") {
		t.Log("User password successfully migrated to bcrypt")
	} else {
		t.Error("User password was NOT migrated to bcrypt")
	}
}
