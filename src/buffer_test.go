package src

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConnectWithRetry(t *testing.T) {
	t.Run("Initial Connection", func(t *testing.T) {
		// Counter for how many times the server has been hit
		hitCount := 0

		// Create a mock server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hitCount++
			if hitCount <= 2 {
				// Fail the first two times
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				// Succeed the third time
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		// Setup settings for retry
		Settings.StreamRetryEnabled = true
		Settings.StreamMaxRetries = 3
		Settings.StreamRetryDelay = 1 // 1 second

		req, _ := http.NewRequest("GET", server.URL, nil)
		client := &http.Client{}

		resp, err := connectWithRetry(client, req)

		if err != nil {
			t.Fatalf("connectWithRetry failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status code %d, but got %d", http.StatusOK, resp.StatusCode)
		}

		if hitCount != 3 {
			t.Errorf("Expected server to be hit 3 times, but got %d", hitCount)
		}
	})
}
