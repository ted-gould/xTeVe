package src

import (
	"testing"
)

func TestResolveHostIPNoPanic(t *testing.T) {
	// Backup original values
	originalSettings := Settings
	originalSystem := System
	defer func() {
		Settings = originalSettings
		System = originalSystem
	}()

	// Reset System and Settings to a clean state for the test
	System = SystemStruct{}
	Settings = SettingsStruct{}

	// This should not panic
	err := resolveHostIP()
	if err != nil {
		t.Fatalf("resolveHostIP() returned an error: %v", err)
	}
}

func TestRandomString(t *testing.T) {
	n := 10
	s, err := randomString(n)
	if err != nil {
		t.Fatalf("randomString returned an error: %v", err)
	}

	if len(s) != n {
		t.Errorf("Expected length %d, got %d", n, len(s))
	}
}
