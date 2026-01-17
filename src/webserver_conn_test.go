package src

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConnStateTracking(t *testing.T) {
	// Reset the counter
	atomic.StoreInt64(&activeHTTPConnections, 0)

	// Create a test server with the connState callback
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ts.Config.ConnState = connState
	ts.Start()
	defer ts.Close()

	// Initial check
	if val := atomic.LoadInt64(&activeHTTPConnections); val != 0 {
		t.Errorf("Expected 0 connections, got %d", val)
	}

	// Make a request
	client := ts.Client()
	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Wait for connection state changes to propagate (async)
	// Since client uses Keep-Alive, connection should remain open (StateIdle)
	// count should be 1.
	time.Sleep(100 * time.Millisecond)
	if val := atomic.LoadInt64(&activeHTTPConnections); val != 1 {
		t.Errorf("Expected 1 connection (idle), got %d", val)
	}

	// Close idle connections
	ts.CloseClientConnections()
	time.Sleep(100 * time.Millisecond)

	if val := atomic.LoadInt64(&activeHTTPConnections); val != 0 {
		t.Errorf("Expected 0 connections after closing, got %d", val)
	}
}

func TestConnStateTracking_Websocket(t *testing.T) {
	// Reset
	atomic.StoreInt64(&activeHTTPConnections, 0)

	// Create a test server that upgrades to websocket
	upgrader := websocket.Upgrader{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Simulate the defer logic in WS handler
		defer atomic.AddInt64(&activeHTTPConnections, -1)
		defer conn.Close()

		// Read message to keep connection open
		_, _, _ = conn.ReadMessage()
	})

	ts := httptest.NewUnstartedServer(handler)
	ts.Config.ConnState = connState
	ts.Start()
	defer ts.Close()

	// Connect with websocket client
	wsURL := "ws" + ts.URL[4:]
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Connection is open. connState(New) -> +1.
	// Upgrade happens. connState(Hijacked) -> no change.
	// We expect 1.
	time.Sleep(100 * time.Millisecond)
	if val := atomic.LoadInt64(&activeHTTPConnections); val != 1 {
		t.Errorf("Expected 1 active websocket connection, got %d", val)
	}

	// Close client
	ws.Close()

	// Server handler should exit ReadMessage with error, trigger defer, and decrement.
	// Wait for propagation
	time.Sleep(200 * time.Millisecond)

	if val := atomic.LoadInt64(&activeHTTPConnections); val != 0 {
		t.Errorf("Expected 0 connections after websocket close, got %d", val)
	}
}

func TestConnStateTracking_Multiple(t *testing.T) {
	atomic.StoreInt64(&activeHTTPConnections, 0)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	ts.Config.ConnState = connState
	ts.Start()
	defer ts.Close()

	// Open 5 connections
	var conns []net.Conn
	for i := 0; i < 5; i++ {
		c, err := net.Dial("tcp", ts.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		// Send a request to ensure it's accepted and state goes New -> Active
		fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	}

	time.Sleep(200 * time.Millisecond)
	if val := atomic.LoadInt64(&activeHTTPConnections); val != 5 {
		t.Errorf("Expected 5 connections, got %d", val)
	}

	// Close 2
	conns[0].Close()
	conns[1].Close()
	time.Sleep(200 * time.Millisecond)

	if val := atomic.LoadInt64(&activeHTTPConnections); val != 3 {
		t.Errorf("Expected 3 connections, got %d", val)
	}

	for _, c := range conns[2:] {
		c.Close()
	}
	time.Sleep(200 * time.Millisecond)
	if val := atomic.LoadInt64(&activeHTTPConnections); val != 0 {
		t.Errorf("Expected 0 connections, got %d", val)
	}
}
