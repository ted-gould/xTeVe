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
	const alphanum = "AB1CD2EF3GH4IJ5KL6MN7OP8QR9ST0UVWXYZ"

	for i := 0; i < 100; i++ {
		n := i + 1
		s, err := randomString(n)
		if err != nil {
			t.Fatalf("randomString returned an error: %v", err)
		}

		if len(s) != n {
			t.Errorf("Expected length %d, got %d", n, len(s))
		}

		for _, char := range s {
			found := false
			for _, allowed := range alphanum {
				if char == allowed {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("randomString returned unexpected character: %c", char)
			}
		}
	}
}
