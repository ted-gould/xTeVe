package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io" // For io.ReadAll
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	xteve "xteve/src"
)

var port = flag.String("port", "", ": Server port          [34400] (default: 34400)")
var host = flag.String("host", "", ": Server host                  (default: localhost)")
var useSocket = flag.Bool("socket", true, ": Use Unix socket         (default: true)")

func main() {
	flag.Parse()

	requestBody, err := json.Marshal(&xteve.APIRequestStruct{
		Cmd: "status",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to marshall request: %v\n", err)
		os.Exit(-1)
	}

	var resp *http.Response

	// Try Unix socket first if enabled
	if *useSocket {
		// Use the same predictable socket path as the server
		socketPath := filepath.Join(os.TempDir(), "xteve.sock")

		// Create HTTP client with Unix socket transport
		client := &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
		}

		// The URL doesn't matter for Unix sockets, but we need a valid URL
		resp, err = client.Post("http://unix/api/", "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			// If socket connection fails, fall back to TCP
			fmt.Fprintf(os.Stderr, "Unix socket connection failed, falling back to TCP: %v\n", err)
			resp = nil
		}
	}

	// Fall back to TCP if socket is disabled or failed
	if resp == nil {
		portNum := 34400
		if port != nil && *port != "" {
			var err error
			portNum, err = strconv.Atoi(*port)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Unable parse port: %v\n", err)
				os.Exit(-1)
			}
		}

		hostname := "localhost"
		if host != nil && *host != "" {
			hostname = *host
		}

		resp, err = http.Post(fmt.Sprintf("http://%s:%d/api/", hostname, portNum), "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to get API: %v\n", err)
			os.Exit(-1)
		}
	}

	defer resp.Body.Close()

	respStr, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable read response: %v\n", err)
		os.Exit(-1)
	}

	var apiresp xteve.APIResponseStruct
	err = json.Unmarshal(respStr, &apiresp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable parse response: %v\n", err)
		fmt.Fprintf(os.Stderr, "%s\n", respStr)
		os.Exit(-1)
	}

	fmt.Printf("xTeVe status:\n")
	fmt.Printf("EPG Source:        %v\n", apiresp.EpgSource)
	fmt.Printf("Error:             %v\n", apiresp.Error)
	fmt.Printf("Status:            %v\n", apiresp.Status)
	fmt.Printf("Streams Active:    %v\n", apiresp.StreamsActive)
	fmt.Printf("Streams Total:     %v\n", apiresp.StreamsAll)
	fmt.Printf("Streams XEPG:      %v\n", apiresp.StreamsXepg)
	fmt.Printf("Tuners Active:     %v\n", apiresp.TunerActive)
	fmt.Printf("Tuners Available:  %v\n", apiresp.TunerAll)
	fmt.Printf("URL for DVR:       %v\n", apiresp.URLDvr)
	fmt.Printf("URL for M3U:       %v\n", apiresp.URLM3U)
	fmt.Printf("URL for XEPG:      %v\n", apiresp.URLXepg)
	fmt.Printf("API Version:       %v\n", apiresp.VersionAPI)
	fmt.Printf("xTeVe Version:     %v\n", apiresp.VersionXteve)

	os.Exit(0)
}
