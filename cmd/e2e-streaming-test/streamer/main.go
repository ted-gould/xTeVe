package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"xteve/src/mpegts"
)

const (
	defaultStreamSize = 1 * 1024 * 1024 // 1 MB
	defaultPort       = 8080
	numStreams        = 4
	chunkSize         = 1024 // 1 KB
	delay             = 10 * time.Millisecond
)

var (
	testData      [][]byte
	activeStreams int64
)

func generateTestData(size, count int) {
	// Ensure the size is a multiple of the packet size
	if size%mpegts.PacketSize != 0 {
		size = (size / mpegts.PacketSize) * mpegts.PacketSize
	}

	fmt.Printf("Generating %d bytes of valid MPEG-TS test data for %d streams...\n", size, count)
	testData = make([][]byte, count)
	for i := 0; i < count; i++ {
		testData[i] = make([]byte, size)
		for j := 0; j < size; j += mpegts.PacketSize {
			packet := testData[i][j : j+mpegts.PacketSize]
			packet[0] = mpegts.SyncByte
			for k := 1; k < mpegts.PacketSize; k++ {
				// Differentiate stream content
				packet[k] = byte((j + k + i) % 256)
			}
		}
	}
}

func streamHandler(w http.ResponseWriter, r *http.Request) {
	streamIDStr := strings.TrimPrefix(r.URL.Path, "/stream/")
	streamID, err := strconv.Atoi(streamIDStr)
	if err != nil || streamID < 1 || streamID > numStreams {
		http.NotFound(w, r)
		return
	}
	streamIndex := streamID - 1

	atomic.AddInt64(&activeStreams, 1)
	defer atomic.AddInt64(&activeStreams, -1)

	w.Header().Set("Content-Type", "video/mpeg")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testData[streamIndex])))

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	data := testData[streamIndex]
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		_, err := w.Write(data[i:end])
		if err != nil {
			log.Printf("Failed to write stream data for stream %d: %v", streamID, err)
			return
		}
		flusher.Flush()
		time.Sleep(delay)
	}
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

	generateTestData(size, numStreams)

	fmt.Printf("Starting streaming server on port %d...\n", port)
	http.HandleFunc("/stream/", streamHandler)
	http.HandleFunc("/test.m3u", func(w http.ResponseWriter, r *http.Request) {
		var m3uContent strings.Builder
		m3uContent.WriteString("#EXTM3U\n")
		for i := 1; i <= numStreams; i++ {
			m3uContent.WriteString(fmt.Sprintf("#EXTINF:-1 tvg-id=\"test.stream.%d\" tvg-name=\"Test Stream %d\" group-title=\"Test\",Test Stream %d\n", i, i, i))
			m3uContent.WriteString(fmt.Sprintf("http://localhost:%d/stream/%d\n", port, i))
		}

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		if _, err := w.Write([]byte(m3uContent.String())); err != nil {
			log.Printf("Failed to write m3u data: %v", err)
		}
	})

	// Endpoint to check connection counts
	http.HandleFunc("/connections/active", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, atomic.LoadInt64(&activeStreams))
	})

	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatalf("Streaming server failed: %v", err)
	}
}
