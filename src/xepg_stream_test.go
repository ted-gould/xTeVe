package src

import (
	"compress/gzip"
	"encoding/xml"
	"os"
	"testing"
	"xteve/src/internal/imgcache"
)

func TestCreateXMLTVFileStreaming(t *testing.T) {
	// Setup Globals
	tmpDir, err := os.MkdirTemp("", "xteve_test_stream")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Save originals
	origSystem := System
	origData := Data
	origSettings := Settings

	defer func() {
		System = origSystem
		Data = origData
		Settings = origSettings
	}()

	// Mock System
	System.Folder.Data = tmpDir + "/"
	System.Folder.ImagesCache = tmpDir + "/images/"
	System.File.XML = tmpDir + "/xteve.xml"
	System.Compressed.GZxml = tmpDir + "/xteve.xml.gz"
	System.Name = "xTeVe Test"
	System.Version = "1.0"
	System.Build = "test"

	if err := os.MkdirAll(System.Folder.ImagesCache, 0755); err != nil {
		t.Fatalf("Failed to create mock images cache: %v", err)
	}

	// Mock Data
	Data.XEPG.Channels = make(map[string]XEPGChannelStruct)
	Data.XEPG.Channels["x-ID.0"] = XEPGChannelStruct{
		XActive:    true,
		XChannelID: "100",
		XName:      "Test Channel",
		XEPG:       "x-ID.0",
		TvgLogo:    "logo.png",
	}

	// Bypass early exit check in createXMLTVFile
	Data.Streams.Active = []any{"dummy"}

	// Mock ImgCache
	Data.Cache.Images, _ = imgcache.New(System.Folder.ImagesCache, "http://localhost/images/", false)

	// Run createXMLTVFile
	err = createXMLTVFile()
	if err != nil {
		t.Fatalf("createXMLTVFile failed: %v", err)
	}

	// Verify XML File exists and is valid
	xmlContent, err := os.ReadFile(System.File.XML)
	if err != nil {
		t.Fatalf("Failed to read generated XML file: %v", err)
	}

	var xmltv XMLTV
	err = xml.Unmarshal(xmlContent, &xmltv)
	if err != nil {
		t.Fatalf("Generated XML is invalid: %v", err)
	}

	if len(xmltv.Channel) != 1 {
		t.Errorf("Expected 1 channel, got %d", len(xmltv.Channel))
	}
	if xmltv.Channel[0].ID != "100" {
		t.Errorf("Expected Channel ID 100, got %s", xmltv.Channel[0].ID)
	}

	// Verify GZ File exists and is valid
	gzFile, err := os.Open(System.Compressed.GZxml)
	if err != nil {
		t.Fatalf("Failed to open generated GZ file: %v", err)
	}
	defer gzFile.Close()

	gzReader, err := gzip.NewReader(gzFile)
	if err != nil {
		t.Fatalf("Failed to create gzip reader: %v", err)
	}
	defer gzReader.Close()

	var xmltvGz XMLTV
	err = xml.NewDecoder(gzReader).Decode(&xmltvGz)
	if err != nil {
		t.Fatalf("Generated GZ XML is invalid: %v", err)
	}

	if len(xmltvGz.Channel) != 1 {
		t.Errorf("Expected 1 channel in GZ, got %d", len(xmltvGz.Channel))
	}
}
