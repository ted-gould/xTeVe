package src

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewHTTPClient returns a new http.Client with cookiejar and redirect limits
func NewHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		Transport: otelhttp.NewTransport(
			http.DefaultTransport,
		),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
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
