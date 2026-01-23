package m3u

import (
	"encoding/json"
	"os" // Replaced io/ioutil with os
	"testing"

	"github.com/stretchr/testify/assert"
)

type M3UStream struct {
	GroupTitle string `json:"group-title"`
	Name       string `json:"name"`
	TvgID      string `json:"tvg-id"`
	TvgLogo    string `json:"tvg-logo"`
	TvgName    string `json:"tvg-name"`
	TvgShift   string `json:"tvg-shift,omitempty"`
	URL        string `json:"url"`
	UUIDKey    string `json:"_uuid.key,omitempty"`
	UUIDValue  string `json:"_uuid.value,omitempty"`
}

func TestMakeInterfaceFromM3U(t *testing.T) {
	// Read playlist
	file := "test_playlist_1.m3u"
	content, err := os.ReadFile(file)
	assert.NoError(t, err, "Should read playlist")

	// Parse playlist into []interface{}
	rawStreams, err := MakeInterfaceFromM3U(content)
	assert.NoError(t, err, "Should parse playlist")

	// Build []M3UStream from []interface{}
	streams := []M3UStream{}
	for _, rawStream := range rawStreams {
		jsonString, err := json.MarshalIndent(rawStream, "", "  ")
		assert.NoError(t, err, "Should convert from interface")

		stream := M3UStream{}
		err = json.Unmarshal(jsonString, &stream)
		assert.NoError(t, err, "Should convert from interface")

		streams = append(streams, stream)
	}

	assert.Len(t, streams, 4, "Should be 4 streams in total")

	tests := []struct {
		name       string
		index      int
		wantName   string
		wantGroup  string
		wantURL    string
		wantTvgID  string
		wantTvgName string
		wantTvgLogo string
		wantTvgShift string
	}{
		{
			name:         "stream 1 with all attributes",
			index:        0,
			wantName:     "Channel 1",
			wantGroup:    "Group 1",
			wantURL:      "http://example.com/stream/1",
			wantTvgID:    "tvg.id.1",
			wantTvgName:  "Channel.1",
			wantTvgLogo:  "https://example/logo.png",
			wantTvgShift: "",
		},
		{
			name:         "stream 2 with EXTGRP group",
			index:        1,
			wantName:     "Channel 2",
			wantGroup:    "Group 2",
			wantURL:      "http://example.com/stream/2",
			wantTvgID:    "tvg.id.2",
			wantTvgName:  "Channel.2",
			wantTvgLogo:  "https://example/logo/2.png",
			wantTvgShift: "",
		},
		{
			name:         "stream 3 with special characters and inherited EXTGRP",
			index:        2,
			wantName:     ",:It's - a difficult name |",
			wantGroup:    "Group 2",
			wantURL:      "http://example.com/stream/3",
			wantTvgID:    "",
			wantTvgName:  "",
			wantTvgLogo:  "",
			wantTvgShift: "",
		},
		{
			name:         "stream 4 with group-title overriding EXTGRP",
			index:        3,
			wantName:     "Channel 4",
			wantGroup:    "Group 4",
			wantURL:      "http://example.com/stream/4",
			wantTvgID:    "tvg.id.4",
			wantTvgName:  "Channel.4",
			wantTvgLogo:  "https://example/logo/4.png",
			wantTvgShift: "-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := streams[tt.index]
			assert.Equal(t, tt.wantName, stream.Name)
			assert.Equal(t, tt.wantGroup, stream.GroupTitle)
			assert.Equal(t, tt.wantURL, stream.URL)
			assert.Equal(t, tt.wantTvgID, stream.TvgID)
			assert.Equal(t, tt.wantTvgName, stream.TvgName)
			assert.Equal(t, tt.wantTvgLogo, stream.TvgLogo)
			assert.Equal(t, tt.wantTvgShift, stream.TvgShift)
		})
	}
}

func TestMakeInterfaceFromM3U_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErr     string
	}{
		{
			name:    "missing EXTM3U header",
			input:   "#EXTINF:0,Channel 1\nhttp://example.com/stream",
			wantErr: "Invalid M3U file, an extended M3U file is required.",
		},
		{
			name:    "HLS playlist with EXT-X-TARGETDURATION",
			input:   "#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10,\nhttp://example.com/segment.ts",
			wantErr: "Invalid M3U file, an extended M3U file is required.",
		},
		{
			name:    "HLS playlist with EXT-X-MEDIA-SEQUENCE",
			input:   "#EXTM3U\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:10,\nhttp://example.com/segment.ts",
			wantErr: "Invalid M3U file, an extended M3U file is required.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MakeInterfaceFromM3U([]byte(tt.input))
			assert.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}
