package src

import (
	"os"
	"testing"
	"xteve/src/internal/authentication"
)

func TestCheckAuthorizationLevel_ErrorPropagation(t *testing.T) {
	// Initialize authentication with a temp file
	tmpfile, err := os.CreateTemp("", "auth.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	err = authentication.Init(tmpfile.Name(), 60)
	if err != nil {
		t.Fatal(err)
	}

	// Call checkAuthorizationLevel with an invalid token
	err = checkAuthorizationLevel("invalid-token", "authentication.web")

	if err == nil {
		t.Error("Expected error, got nil")
	} else if err.Error() != "No user id found for this token" {
        t.Errorf("Expected 'No user id found for this token', got '%v'", err)
    }
}
