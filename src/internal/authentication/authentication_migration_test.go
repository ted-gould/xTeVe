package authentication

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func legacyTestHash(secret string) string {
	key := []byte(secret)
	h := hmac.New(sha256.New, key)
	h.Write([]byte("_remote_db"))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func TestMigrationFromLegacyToBcrypt(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "auth_migration_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbFile := filepath.Join(tmpDir, "authentication.json")

	// Create legacy user data
	username := "legacyUser"
	password := "legacyPass"
	salt := "randomSalt123"

	usernameHash := legacyTestHash(username)
	passwordHash := legacyTestHash(password)

	userData := map[string]any{
		"_id":       "id-legacy",
		"_username": usernameHash,
		"_password": passwordHash,
		"_salt":     salt,
		"data":      map[string]any{},
	}

	dbData := map[string]any{
		"dbVersion": "1.0",
		"hash":      "sha256",
		"users": map[string]any{
			"id-legacy": userData,
		},
	}

	jsonData, _ := json.Marshal(dbData)
	err = os.WriteFile(dbFile, jsonData, 0600)
	if err != nil {
		t.Fatal(err)
	}

	// Initialize Authentication system
	err = Init(dbFile, 60)
	if err != nil {
		t.Fatal(err)
	}

	// Authenticate (should trigger migration)
	token, err := UserAuthentication(username, password)
	if err != nil {
		t.Fatalf("Authentication failed for legacy user: %v", err)
	}
	if token == "" {
		t.Fatal("Token is empty")
	}

	// Read the database file to verify migration
	content, err := os.ReadFile(dbFile)
	if err != nil {
		t.Fatal(err)
	}

	var newDBData map[string]any
	err = json.Unmarshal(content, &newDBData)
	if err != nil {
		t.Fatal(err)
	}

	users := newDBData["users"].(map[string]any)
	user := users["id-legacy"].(map[string]any)
	newPasswordHash := user["_password"].(string)

	if !strings.HasPrefix(newPasswordHash, "$2") {
		t.Errorf("Password was not migrated to bcrypt. Hash: %s", newPasswordHash)
	}

    // Verify username hash also migrated (to per-user salt)
    // We can't verify the exact value easily without reimplementing the per-user salt HMAC,
    // but we can check it's NOT the legacy hash anymore.
    newUsernameHash := user["_username"].(string)
    if newUsernameHash == usernameHash {
         t.Errorf("Username hash was not migrated. Still: %s", newUsernameHash)
    }
}
