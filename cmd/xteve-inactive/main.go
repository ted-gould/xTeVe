package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"

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

	respStr, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(errWriter, "Unable read response: %v\n", err)
		return -1
	}

	var apiresp xteve.APIResponseStruct
	err = json.Unmarshal(respStr, &apiresp)
	if err != nil {
		if string(respStr) == "Locked [423]" {
			return 1
		} else {
			fmt.Fprintf(errWriter, "Unable parse response: %v\n", err)
			fmt.Fprintf(errWriter, "%s\n", respStr)
			return -1
		}
	}

	return int(apiresp.TunerActive)
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
