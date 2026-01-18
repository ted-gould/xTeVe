package src

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSSRFProtection(t *testing.T) {
	// Start a local server (Loopback)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// 1. Verify protection is ACTIVE (default)
	os.Unsetenv("XTEVE_ALLOW_LOOPBACK")
	client := NewHTTPClient()
	req, _ := http.NewRequest("GET", ts.URL, nil)

	_, err := client.Do(req)
	if err == nil {
		t.Fatal("Request to loopback SUCCEEDED, but expected it to fail (SSRF protection missing)")
	}
	if !strings.Contains(err.Error(), "access to loopback address") {
		t.Fatalf("Expected 'access to loopback address' error, got: %v", err)
	}

	// 2. Verify bypass is working (for tests)
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
	defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request to loopback FAILED with bypass enabled: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
	}
}
