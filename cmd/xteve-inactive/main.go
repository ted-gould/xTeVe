package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io" // Added for io.ReadAll
	"net/http"
	"os"
	"strconv"
	"strings"

	xteve "xteve/src"
)

var port = flag.String("port", "", ": Server port          [34400] (default: 34400)")
var host = flag.String("host", "", ": Server host                  (default: localhost)")

func runLogic(cmdHost, cmdPort string, outWriter io.Writer, errWriter io.Writer) int {
	portNum := 34400
	if cmdPort != "" {
		var err error
		portNum, err = strconv.Atoi(cmdPort)
		if err != nil {
			fmt.Fprintf(errWriter, "Unable parse port: %v\n", err)
			return -1
		}
	}

	hostname := "localhost"
	if cmdHost != "" {
		hostname = cmdHost
	}

	requestBody, err := json.Marshal(&xteve.APIRequestStruct{
		Cmd: "status",
	})
	if err != nil {
		fmt.Fprintf(errWriter, "Unable to marshall request: %v\n", err)
		return -1
	}

	resp, err := http.Post(fmt.Sprintf("http://%s:%d/api/", hostname, portNum), "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		fmt.Fprintf(errWriter, "Unable to get API: %v\n", err)
		return -1
	}

	defer resp.Body.Close()

	respStr, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(errWriter, "Unable read response: %v\n", err)
		return -1
	}

	var apiresp xteve.APIResponseStruct
	err = json.Unmarshal(respStr, &apiresp)
	if err != nil {
		if strings.TrimSpace(string(respStr)) == "Locked [423]" {
			return 1
		} else {
			fmt.Fprintf(errWriter, "Unable parse response: %v\n", err)
			fmt.Fprintf(errWriter, "%s\n", respStr)
			return -1
		}
	}

	// ActiveHTTPConnections includes the current request, so we subtract 1.
	// If the user has other connections (WebDAV, Web UI, etc.), the count will be > 1.
	// We use max(0, ...) for safety, though it should be >= 1.
	httpActive := apiresp.ActiveHTTPConnections
	if httpActive > 0 {
		httpActive--
	}
	return int(apiresp.TunerActive) + int(httpActive)
}

func main() {
	flag.Parse()

	cmdPort := "34400"
	if port != nil && *port != "" {
		cmdPort = *port
	}

	cmdHost := "localhost"
	if host != nil && *host != "" {
		cmdHost = *host
	}

	exitCode := runLogic(cmdHost, cmdPort, os.Stdout, os.Stderr)
	os.Exit(exitCode)
}
