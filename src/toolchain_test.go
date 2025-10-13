package src

import (
	"bytes"
	"log"
	"net"
	"os"
	"strings"
	"testing"
)

func TestResolveHostIPNoPanic(t *testing.T) {
	// Keep a backup of the original function
	originalNetInterfaceAddrs := netInterfaceAddrs
	defer func() {
		netInterfaceAddrs = originalNetInterfaceAddrs
	}()

	// Override the function to return an empty list of addresses
	netInterfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{}, nil
	}

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

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(os.Stderr)
	}()

	err := resolveHostIP()
	if err != nil {
		t.Fatalf("resolveHostIP() returned an error: %v", err)
	}

	if Settings.HostIP != "127.0.0.1" {
		t.Errorf("Expected HostIP to be '127.0.0.1', but got '%s'", Settings.HostIP)
	}

	if !strings.Contains(buf.String(), "[WARNING] No IP address found, defaulting to 127.0.0.1") {
		t.Errorf("Expected log message not found. Log output: %s", buf.String())
	}
}
