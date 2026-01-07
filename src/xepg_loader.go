package src

import (
	"encoding/json"
	"io"
	"log"
	"os"
)

// loadXEPGChannels loads XEPG channels from a JSON file directly into map[string]XEPGChannelStruct
func loadXEPGChannels(file string) (channels map[string]XEPGChannelStruct, err error) {
	channels = make(map[string]XEPGChannelStruct)
	f, err := os.Open(getPlatformFile(file))
	if err != nil {
		return channels, err // Return empty map and error (e.g. file not found)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return channels, err
	}

	if len(content) > 0 {
		err = json.Unmarshal(content, &channels)
	}

	if err == nil {
		if closeErr := f.Close(); closeErr != nil {
			log.Printf("Error closing file %s: %v", file, closeErr)
			return channels, closeErr
		}
	}
	return
}
