package sources

import (
	"context"
	"encoding/json"
	"fmt"
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
