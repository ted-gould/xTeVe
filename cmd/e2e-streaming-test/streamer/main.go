package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
)

const (
	defaultStreamSize = 10 * 1024 * 1024 // 10 MB
	defaultPort       = 8080
)

var testData []byte

func generateTestData(size int) error {
	fmt.Printf("Generating %d bytes of test data...\n", size)
	testData = make([]byte, size)
	for i := 0; i < size; i++ {
		testData[i] = byte(i % 256)
	}
	return nil
}

func main() {
	portStr := os.Getenv("STREAMER_PORT")
	port, err := strconv.Atoi(portStr)
	if err != nil || port == 0 {
		port = defaultPort
	}

	sizeStr := os.Getenv("STREAMER_SIZE")
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size == 0 {
		size = defaultStreamSize
	}

	if err := generateTestData(size); err != nil {
		log.Fatalf("Failed to generate test data: %v", err)
	}

	fmt.Printf("Starting streaming server on port %d...\n", port)
	http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mpeg")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData)))
		if _, err := w.Write(testData); err != nil {
			log.Printf("Failed to write stream data: %v", err)
		}
	})
	http.HandleFunc("/test.m3u", func(w http.ResponseWriter, r *http.Request) {
		m3uContent := fmt.Sprintf(`#EXTM3U
#EXTINF:-1 tvg-id="test.stream" tvg-name="Test Stream" group-title="Test",Test Stream
http://localhost:%d/stream
`, port)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if _, err := w.Write([]byte(m3uContent)); err != nil {
			log.Printf("Failed to write m3u data: %v", err)
		}
	})

	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatalf("Streaming server failed: %v", err)
	}
}
