package src

import (
	"fmt"
	"net/http"
	"time"
)

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
