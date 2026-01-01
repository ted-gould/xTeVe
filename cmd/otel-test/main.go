package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

// TraceCollector listens for OTLP traces.
type TraceCollector struct {
	mu          sync.Mutex
	traceCount  int
	server      *http.Server
	port        int
}

func (tc *TraceCollector) Start() error {
	// Listen on a random available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	tc.port = listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", tc.handleTraces)

	tc.server = &http.Server{
		Handler: mux,
	}

	go func() {
		if err := tc.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Trace collector server error: %v", err)
		}
	}()

	return nil
}

func (tc *TraceCollector) Stop() {
	if tc.server != nil {
		tc.server.Close()
	}
}

func (tc *TraceCollector) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Simply count the request as a received trace batch
	// In a real scenario, we might parse the JSON to verify content
	tc.mu.Lock()
	tc.traceCount++
	tc.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (tc *TraceCollector) GetTraceCount() int {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.traceCount
}

func main() {
	if err := run(); err != nil {
		log.Printf("OTEL integration test failed: %v", err)
		os.Exit(1)
	}
	fmt.Println("OTEL integration test completed successfully!")
}

func run() error {
	// 1. Start OTLP trace collector
	collector := &TraceCollector{}
	if err := collector.Start(); err != nil {
		return fmt.Errorf("failed to start trace collector: %w", err)
	}
	defer collector.Stop()

	collectorURL := fmt.Sprintf("http://127.0.0.1:%d", collector.port)
	fmt.Printf("Trace collector started on %s\n", collectorURL)

	// 2. Start xTeVe with OTLP configuration
	cmd, err := startXteve(collectorURL)
	if err != nil {
		return fmt.Errorf("failed to start xteve: %w", err)
	}
	defer stopXteve(cmd)

	// Wait for server to be ready
	xteveURL := "http://localhost:34400/web/"
	if err := waitForServerReady(xteveURL); err != nil {
		return fmt.Errorf("xteve server not ready: %w", err)
	}

	// 3. Make a request to xTeVe to generate a trace
	fmt.Println("Sending request to xTeVe...")
	resp, err := http.Get(xteveURL)
	if err != nil {
		return fmt.Errorf("failed to make request to xteve: %w", err)
	}
	resp.Body.Close()

	// 4. Verify traces received
	fmt.Println("Waiting for traces...")
	if err := waitForTraces(collector); err != nil {
		return fmt.Errorf("trace verification failed: %w", err)
	}

	return nil
}

func startXteve(collectorEndpoint string) (*exec.Cmd, error) {
	fmt.Println("Building and starting xteve server...")
	// Build xteve
	buildCmd := exec.Command("go", "build", "-o", "xteve_otel_test", "xteve.go")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("failed to build xteve: %w\n%s", err, string(out))
	}

	// Clean config
	os.RemoveAll(".xteve_otel")

	cmd := exec.Command("./xteve_otel_test", "-port=34400", "-config=.xteve_otel")

	// Set OTLP environment variables
	env := os.Environ()
	env = append(env, "OTEL_EXPORTER_TYPE=otlp-http")
	env = append(env, fmt.Sprintf("OTEL_EXPORTER_OTLP_ENDPOINT=%s", collectorEndpoint))
	// Set headers if needed, but for local simple test it might not be required if we don't check authentication
	// We need to make sure the library sends traces insecurely (http) which is implied by the scheme

	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func stopXteve(cmd *exec.Cmd) {
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
}

func waitForServerReady(url string) error {
	fmt.Println("Waiting for xTeVe to be ready...")
	for i := 0; i < 30; i++ {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timeout waiting for server")
}

func waitForTraces(collector *TraceCollector) error {
	// Wait up to 10 seconds for traces
	for i := 0; i < 20; i++ {
		count := collector.GetTraceCount()
		if count > 0 {
			fmt.Printf("Received %d traces.\n", count)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("no traces received after timeout")
}
