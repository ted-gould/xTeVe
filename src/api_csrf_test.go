package src

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAPICSRF_ContentType(t *testing.T) {
	// Bypass SSRF protection for local testing if needed
	os.Setenv("XTEVE_ALLOW_LOOPBACK", "true")

	// 1. Vulnerable Request (text/plain)
	jsonBody := []byte(`{"cmd":"status"}`)
	req := httptest.NewRequest("POST", "/api/", bytes.NewBuffer(jsonBody))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "text/plain") // Vulnerable Content-Type

	w := httptest.NewRecorder()
	API(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Logf("Expected 415 Unsupported Media Type, got %d", resp.StatusCode)
		t.Fail()
	} else {
		t.Log("Got 415 Unsupported Media Type as expected (CSRF Protected)")
	}

	// 2. Valid Request (application/json)
	req2 := httptest.NewRequest("POST", "/api/", bytes.NewBuffer(jsonBody))
	req2.RemoteAddr = "127.0.0.1:1234"
	req2.Header.Set("Content-Type", "application/json") // Correct Content-Type

	w2 := httptest.NewRecorder()
	API(w2, req2)

	resp2 := w2.Result()
	if resp2.StatusCode != http.StatusOK {
		t.Logf("Expected 200 OK for valid content type, got %d", resp2.StatusCode)
		t.Fail()
	} else {
		t.Log("Got 200 OK for valid content type")
	}
}
