package src

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"sync"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	xTeVeTransport *http.Transport
	transportOnce  sync.Once
)

func getXTeVeTransport() *http.Transport {
	transportOnce.Do(func() {
		if t, ok := http.DefaultTransport.(*http.Transport); ok {
			xTeVeTransport = t.Clone()
		} else {
			xTeVeTransport = &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			}
		}
		xTeVeTransport.DialContext = dialContextWithRetry
	})
	return xTeVeTransport
}

func dialContextWithRetry(ctx context.Context, network, addr string) (net.Conn, error) {
	var conn net.Conn
	var err error

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			if os.Getenv("XTEVE_ALLOW_LOOPBACK") == "true" || os.Getenv("XTEVE_ALLOW_LOOPBACK") == "1" {
				return nil
			}
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("invalid IP: %s", host)
			}
			if ip.IsLoopback() {
				return fmt.Errorf("access to loopback address %s is denied", host)
			}
			if ip.IsLinkLocalUnicast() {
				return fmt.Errorf("access to link-local address %s is denied", host)
			}
			if ip.IsUnspecified() {
				return fmt.Errorf("access to unspecified address %s is denied", host)
			}
			return nil
		},
	}

	// Retry loop for transient errors (like DNS "server misbehaving")
	for i := 0; i < 3; i++ {
		conn, err = dialer.DialContext(ctx, network, addr)
		if err == nil {
			return conn, nil
		}

		// Wait before retry
		if i < 2 {
			select {
			case <-ctx.Done():
				return nil, err
			case <-time.After(200 * time.Millisecond):
				continue
			}
		}
	}
	return nil, err
}

// NewHTTPClient returns a new http.Client with cookiejar and redirect limits
func NewHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		Transport: otelhttp.NewTransport(
			getXTeVeTransport(),
		),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}

			// Add attributes and event to the current span if it exists.
			// req.Context() inherits the context from the original request.
			span := trace.SpanFromContext(req.Context())
			if span.IsRecording() {
				// Record the number of redirects encountered so far.
				// via contains the requests that have already been made.
				span.SetAttributes(attribute.Int("http.redirect_count", len(via)))

				// Record the redirect event with the target location.
				span.AddEvent("http.redirect", trace.WithAttributes(
					attribute.String("http.redirect.location", req.URL.String()),
				))
			}

			return nil
		},
	}
}

func ConnectWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error
	var retries = 0

	for {
		resp, err = client.Do(req)

		if err != nil {
			if resp != nil {
				debugResponse(resp)
			}
			if Settings.StreamRetryEnabled && retries < Settings.StreamMaxRetries {
				retries++
				showInfo(fmt.Sprintf("Stream Error (%s). Retry %d/%d in %d milliseconds.", err.Error(), retries, Settings.StreamMaxRetries, Settings.StreamRetryDelay))
				time.Sleep(time.Duration(Settings.StreamRetryDelay) * time.Millisecond)
				continue
			}
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			if Settings.StreamRetryEnabled && retries < Settings.StreamMaxRetries {
				retries++
				showInfo(fmt.Sprintf("Stream HTTP Status Error (%s). Retry %d/%d in %d milliseconds.", http.StatusText(resp.StatusCode), retries, Settings.StreamMaxRetries, Settings.StreamRetryDelay))
				time.Sleep(time.Duration(Settings.StreamRetryDelay) * time.Millisecond)
				continue
			}
			return resp, fmt.Errorf("bad status: %s", resp.Status)
		}

		return resp, nil
	}
}
