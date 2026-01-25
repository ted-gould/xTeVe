package src

import (
	"os"
	"testing"
)

func TestGetGuideNumberPMS(t *testing.T) {
	// Save original Data.Cache.PMS and System.File.PMS
	originalPMS := make(map[string]string)
	for k, v := range Data.Cache.PMS {
		originalPMS[k] = v
	}
	originalPMSFile := System.File.PMS

	// Create temp file for PMS
	tmpFile, err := os.CreateTemp("", "pms_test_*.json")
	if err != nil {
		t.Fatal(err)
	}
	// Initialize with empty JSON object
	if _, err := tmpFile.Write([]byte("{}")); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	System.File.PMS = tmpFile.Name()

	defer func() {
		// Restore original state
		Data.Cache.PMS = originalPMS
		System.File.PMS = originalPMSFile
	}()

	tests := []struct {
		name     string
		setup    map[string]string
		wantID   string
	}{
		{
			name:   "Empty cache",
			setup:  map[string]string{},
			wantID: "id-0",
		},
		{
			name: "Cache has id-0",
			setup: map[string]string{
				"channel1": "id-0",
			},
			wantID: "id-1",
		},
		{
			name: "Cache has id-0 and id-2 (gap)",
			setup: map[string]string{
				"channel1": "id-0",
				"channel2": "id-2",
			},
			wantID: "id-1",
		},
		{
			name: "Cache has id-0 and id-1",
			setup: map[string]string{
				"channel1": "id-0",
				"channel2": "id-1",
			},
			wantID: "id-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			Data.Cache.PMS = make(map[string]string)
			for k, v := range tt.setup {
				Data.Cache.PMS[k] = v
			}

			// Call with a new channel name
			gotID, _ := getGuideNumberPMS("newChannel")
			// We ignore error from saveMapToJSONFile as we just test logic

			if gotID != tt.wantID {
				t.Errorf("getGuideNumberPMS() = %v, want %v", gotID, tt.wantID)
			}
		})
	}
}
