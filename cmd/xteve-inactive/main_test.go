package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"xteve/src"
)

func TestRunLogic_LockedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/" {
			t.Fatalf("Unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var reqBody src.APIRequestStruct
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		if reqBody.Cmd != "status" {
			t.Fatalf("Expected Cmd 'status', got '%s'", reqBody.Cmd)
		}
		fmt.Fprint(w, "Locked [423]")
	}))
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("Failed to parse server URL: %v", err)
	}
	host, port := parsedURL.Hostname(), parsedURL.Port()

	var outBuf, errBuf bytes.Buffer
	exitCode := runLogic(host, port, &outBuf, &errBuf)

	if exitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", exitCode)
	}
	if errBuf.String() != "" {
		t.Errorf("Expected empty stderr, got: %s", errBuf.String())
	}
	if outBuf.String() != "" {
		t.Errorf("Expected empty stdout, got: %s", outBuf.String())
	}
}

func TestRunLogic_InvalidJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Not JSON {")
	}))
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("Failed to parse server URL: %v", err)
	}
	host, port := parsedURL.Hostname(), parsedURL.Port()

	var outBuf, errBuf bytes.Buffer
	exitCode := runLogic(host, port, &outBuf, &errBuf)

	if exitCode != -1 {
		t.Errorf("Expected exit code -1, got %d", exitCode)
	}
	expectedErr := "Unable parse response:"
	if !strings.Contains(errBuf.String(), expectedErr) {
		t.Errorf("Expected stderr to contain '%s', got: %s", expectedErr, errBuf.String())
	}
	expectedErrBody := "Not JSON {"
	if !strings.Contains(errBuf.String(), expectedErrBody) {
		t.Errorf("Expected stderr to contain '%s', got: %s", expectedErrBody, errBuf.String())
	}
	if outBuf.String() != "" {
		t.Errorf("Expected empty stdout, got: %s", outBuf.String())
	}
}

func TestRunLogic_TunerInactive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := src.APIResponseStruct{TunerActive: 0}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("Failed to parse server URL: %v", err)
	}
	host, port := parsedURL.Hostname(), parsedURL.Port()

	var outBuf, errBuf bytes.Buffer
	exitCode := runLogic(host, port, &outBuf, &errBuf)

	if exitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", exitCode)
	}
	if errBuf.String() != "" {
		t.Errorf("Expected empty stderr, got: %s", errBuf.String())
	}
	if outBuf.String() != "" {
		t.Errorf("Expected empty stdout, got: %s", outBuf.String())
	}
}

func TestRunLogic_TunerActive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := src.APIResponseStruct{TunerActive: 1}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("Failed to parse server URL: %v", err)
	}
	host, port := parsedURL.Hostname(), parsedURL.Port()

	var outBuf, errBuf bytes.Buffer
	exitCode := runLogic(host, port, &outBuf, &errBuf)

	if exitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", exitCode)
	}
	if errBuf.String() != "" {
		t.Errorf("Expected empty stderr, got: %s", errBuf.String())
	}
	if outBuf.String() != "" {
		t.Errorf("Expected empty stdout, got: %s", outBuf.String())
	}
}

func TestRunLogic_ServerDown(t *testing.T) {
	var outBuf, errBuf bytes.Buffer
	// Attempt to connect to a port that is presumably not listening
	exitCode := runLogic("localhost", "1", &outBuf, &errBuf)

	if exitCode != -1 {
		t.Errorf("Expected exit code -1, got %d", exitCode)
	}
	expectedErr := "Unable to get API:"
	if !strings.Contains(errBuf.String(), expectedErr) {
		t.Errorf("Expected stderr to contain '%s', got: %s", expectedErr, errBuf.String())
	}
	if outBuf.String() != "" {
		t.Errorf("Expected empty stdout, got: %s", outBuf.String())
	}
}
