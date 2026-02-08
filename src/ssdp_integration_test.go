package src

import (
	"testing"
	"time"

	"github.com/koron/go-ssdp"
	"github.com/stretchr/testify/assert"
)

func TestSSDPAdvertisement(t *testing.T) {
	// Setup global variables
	Settings.SSDP = true
	System.Flag.Info = false
	System.DeviceID = "test-device-id"
	System.URLBase = "http://localhost:34400"
	System.AppName = "xTeVe-Test"

	// Start SSDP
	err := SSDP()
	assert.NoError(t, err)

	// Wait for SSDP to start up
	time.Sleep(1 * time.Second)

	// Search for Root Device
	listRoot, err := ssdp.Search("upnp:rootdevice", 1, "")
	if err != nil {
		t.Logf("Search root error: %v", err)
	}

	foundRoot := false
	for _, srv := range listRoot {
		t.Logf("Found Service: Type=%s USN=%s Location=%s", srv.Type, srv.USN, srv.Location)
		if srv.Type == "upnp:rootdevice" && srv.USN == "uuid:test-device-id::upnp:rootdevice" {
			foundRoot = true
		}
	}

	// Search for WebDAV Service
	listDav, err := ssdp.Search("urn:schemas-upnp-org:service:WebDAV:1", 1, "")
	if err != nil {
		t.Logf("Search dav error: %v", err)
	}

	foundDav := false
	for _, srv := range listDav {
		t.Logf("Found Service: Type=%s USN=%s Location=%s", srv.Type, srv.USN, srv.Location)
		if srv.Type == "urn:schemas-upnp-org:service:WebDAV:1" && srv.USN == "uuid:test-device-id::urn:schemas-upnp-org:service:WebDAV:1" {
			foundDav = true
		}
	}

	if !foundRoot {
		t.Error("Did not find upnp:rootdevice via Search")
	}
	if !foundDav {
		t.Error("Did not find urn:schemas-upnp-org:service:WebDAV:1 via Search")
	}
}
