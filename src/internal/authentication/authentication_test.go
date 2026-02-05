package authentication

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckTheValidityOfTheTokenFromHTTPHeader(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "auth_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "auth.json")
	err = Init(dbPath, 60)
	if err != nil {
		t.Fatal(err)
	}

	// Create a user to get a valid token
	// We need to clear data map first to ensure clean state if other tests ran
	// But Init does loadDatabase which overwrites data if file exists, or we create fresh.
	// Since tmpDir is fresh, Init creates fresh DB.

	// However, we need to ensure 'data' global is consistent.
	// CreateDefaultUser checks if len(users) > 0.

	err = CreateDefaultUser("admin", "password")
	if err != nil {
		t.Fatalf("CreateDefaultUser failed: %v", err)
	}
	validToken, err := UserAuthentication("admin", "password")
	if err != nil {
		t.Fatalf("UserAuthentication failed: %v", err)
	}

	tests := []struct {
		name          string
		cookies       []*http.Cookie
		expectError   bool
		errorContains string
	}{
		{
			name:          "NoCookies",
			cookies:       []*http.Cookie{},
			expectError:   true,
			errorContains: "Session has expired",
		},
		{
			name: "ValidTokenCookie",
			cookies: []*http.Cookie{
				{Name: "Token", Value: validToken},
			},
			expectError: false,
		},
		{
			name: "InvalidTokenCookie",
			cookies: []*http.Cookie{
				{Name: "Token", Value: "invalid-token"},
			},
			expectError:   true,
			errorContains: "Session has expired",
		},
		{
			name: "OtherCookieOnly",
			cookies: []*http.Cookie{
				{Name: "Other", Value: "something"},
			},
			expectError:   true,
			errorContains: "Session has expired",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for _, c := range tt.cookies {
				req.AddCookie(c)
			}
			w := httptest.NewRecorder()

			_, _, err := CheckTheValidityOfTheTokenFromHTTPHeader(w, req, false)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, got nil")
				} else if tt.errorContains != "" && err.Error() != tt.errorContains {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}
