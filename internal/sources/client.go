package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

// pretend to be a normal browser, some boards 403 the default
// "Go-http-client/1.1" user agent on sight
const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"

// NewHTTPClient builds the ONE client the whole app shares.
// http.Client is safe for concurrent use and reusing it keeps
// connections alive between requests instead of redoing tcp+tls
// handshakes for all ~50 boards every tick.
// retryablehttp wraps it with exponential backoff on 429/5xx
func NewHTTPClient() *http.Client {
	rc := retryablehttp.NewClient()
	rc.RetryMax = 3
	rc.RetryWaitMin = 1 * time.Second
	rc.RetryWaitMax = 30 * time.Second
	// per-attempt timeout so one hung board can't stall a whole tick
	rc.HTTPClient.Timeout = 15 * time.Second
	// we log per source ourselves, retryablehttp's own logging is noise
	rc.Logger = nil
	return rc.StandardClient()
}

// the google careers page runs ~1.3MB, cap reads so a misbehaving
// page can't balloon memory
const maxHTMLBytes = 8 << 20

// getHTML does a GET with browser headers and returns the body, for
// career sites (google, apple) that embed their job json in the page
func getHTML(ctx context.Context, c *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxHTMLBytes))
	if err != nil {
		return "", fmt.Errorf("GET %s: reading body: %w", url, err)
	}
	return string(b), nil
}

// getJSON does a GET with browser headers and decodes the response
// body into v. all four source clients funnel through here so the
// header/status/decode handling lives in exactly one place
func getJSON(ctx context.Context, c *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	// always close the body or the connection can't be reused
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("GET %s: decoding json: %w", url, err)
	}
	return nil
}

// postJSON does a POST with a json body and decodes the response into
// v. workday's cxs endpoints only answer to POST, everything else in
// the app is GET, so this is the one place that speaks it.
// retryablehttp buffers the body so it still replays on a 429/5xx retry
func postJSON(ctx context.Context, c *http.Client, url string, body, v any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s: unexpected status %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("POST %s: decoding json: %w", url, err)
	}
	return nil
}
