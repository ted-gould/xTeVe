package authentication

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"path/filepath"
	"testing"
)

// legacyHashForTest duplicates the insecure hashing logic (ignoring salt)
func legacyHashForTest(secret string) string {
	key := []byte(secret)
	h := hmac.New(sha256.New, key)
	h.Write([]byte("_remote_db"))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// newHashForTest duplicates the secure hashing logic (using salt)
func newHashForTest(secret, salt string) string {
	key := []byte(secret)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(salt))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func TestMigration(t *testing.T) {
	// Reset global state
	initAuthentication = false
	data = make(map[string]any)
	tokens = make(map[string]any)

	tmpDir := t.TempDir()
	// Create a dummy file because Init expects the directory to exist
	// and Init takes the *file* path.
	dbPath := filepath.Join(tmpDir, "auth.json")

	// Init
	err := Init(dbPath, 60)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	username := "migration_user"
	password := "secret123"
	salt := "random_salt_value_12345"

	// 1. Manually create user with LEGACY hash
	uHashLegacy := legacyHashForTest(username)
	pHashLegacy := legacyHashForTest(password)

	userData := make(map[string]any)
	userData["_username"] = uHashLegacy
	userData["_password"] = pHashLegacy
	userData["_salt"] = salt
	userData["_id"] = "id-12345"
	userData["data"] = make(map[string]any)

	users := data["users"].(map[string]any)
	users["id-12345"] = userData

	// Save the database with legacy data
	err = saveDatabase(data)
	if err != nil {
		t.Fatalf("Failed to save database: %v", err)
	}

	// 2. Attempt Login - This should trigger migration
	token, err := UserAuthentication(username, password)
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if token == "" {
		t.Fatal("Token is empty")
	}

	// 3. Verify Migration
	// Reload database from disk to ensure persistence
	err = loadDatabase()
	if err != nil {
		t.Fatalf("Failed to load database: %v", err)
	}

	users = data["users"].(map[string]any)
	updatedUser := users["id-12345"].(map[string]any)

	currentPHash := updatedUser["_password"].(string)
	currentUHash := updatedUser["_username"].(string)

	expectedPHash := newHashForTest(password, salt)
	expectedUHash := newHashForTest(username, salt)

	// Check if hashes are updated to the secure version
	if currentPHash == pHashLegacy {
		t.Error("Password hash was NOT migrated (still matches legacy hash)")
	} else if currentPHash != expectedPHash {
		// This branch will likely be taken before the fix, as current logic won't change it to expectedPHash
		// But current logic also won't change it at all, so it should match pHashLegacy.
		// Wait, if I haven't implemented migration, currentPHash == pHashLegacy.
		// So the first 'if' will catch it.
		t.Errorf("Password hash does not match expected new hash. Got %s, want %s", currentPHash, expectedPHash)
	} else {
		t.Log("Password hash successfully migrated.")
	}

	if currentUHash == uHashLegacy {
		t.Error("Username hash was NOT migrated (still matches legacy hash)")
	} else if currentUHash != expectedUHash {
		t.Errorf("Username hash does not match expected new hash. Got %s, want %s", currentUHash, expectedUHash)
	} else {
		t.Log("Username hash successfully migrated.")
	}

	// 4. Verify Login works with NEW hash
	// Reset tokens to force re-login
	tokens = make(map[string]any)
	token2, err := UserAuthentication(username, password)
	if err != nil {
		t.Fatalf("Subsequent login failed: %v", err)
	}
	if token2 == "" {
		t.Fatal("Subsequent token is empty")
	}
}
