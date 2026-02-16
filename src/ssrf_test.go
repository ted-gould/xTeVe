package src

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNewHTTPClient_SSRF(t *testing.T) {
	// Start a local test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	originalEnv := os.Getenv("XTEVE_ALLOW_LOOPBACK")
	defer os.Setenv("XTEVE_ALLOW_LOOPBACK", originalEnv)

	t.Run("Access Localhost Blocked", func(t *testing.T) {
		// Ensure environment variable is unset
		os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

		client := NewHTTPClient()
		resp, err := client.Get(ts.URL)

		if err == nil {
			if resp != nil {
				resp.Body.Close()
			}
			t.Error("Accessing localhost should fail with SSRF protection")
		} else {
			if !strings.Contains(err.Error(), "SSRF protection") {
				t.Errorf("Expected SSRF protection error, got: %v", err)
			}
		}
	})

	t.Run("Access Unspecified Blocked", func(t *testing.T) {
		// Ensure environment variable is unset
		os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

		// Construct 0.0.0.0 URL from ts.URL
		// ts.URL is http://127.0.0.1:port
		parts := strings.Split(ts.URL, ":")
		port := parts[len(parts)-1]
		zeroURL := "http://0.0.0.0:" + port

		client := NewHTTPClient()
		resp, err := client.Get(zeroURL)

		if err == nil {
			if resp != nil {
				resp.Body.Close()
			}
			t.Error("Accessing 0.0.0.0 should fail with SSRF protection")
		} else {
			if !strings.Contains(err.Error(), "SSRF protection") {
				t.Errorf("Expected SSRF protection error, got: %v", err)
			}
		}
	})

	t.Run("Access Localhost Allowed via Env", func(t *testing.T) {
		// Set environment variable to allow loopback
		os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")
		defer os.Unsetenv("XTEVE_ALLOW_LOOPBACK")

		client := NewHTTPClient()
		resp, err := client.Get(ts.URL)

		if err != nil {
			t.Errorf("Accessing localhost should succeed with XTEVE_ALLOW_LOOPBACK=true, got error: %v", err)
		}
		if resp != nil {
			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status OK, got %v", resp.Status)
			}
			resp.Body.Close()
		}
	})
}
