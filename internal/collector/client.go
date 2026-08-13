package collector

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a small HTTP helper with a fixed timeout, used for polling the
// daemons. No external deps.
type Client struct {
	hc      *http.Client
	timeout time.Duration
}

func NewClient(timeout time.Duration) *Client {
	return &Client{
		hc:      &http.Client{},
		timeout: timeout,
	}
}

// Get fetches a URL and returns the body. A non-2xx status is an error.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get %s: status %d", url, resp.StatusCode)
	}
	return body, nil
}
