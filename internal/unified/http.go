package unified

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// HTTPClient sends data to the HTTP entry point. The Unified Endpoint
// processes the body according to its Content-Type.
//
// The underlying http.Client keeps connections alive (HTTP/1.1 default; the
// endpoint's keep-alive timeout is 65s), so repeated Send calls reuse one TCP
// connection unless CloseConnection is set.
type HTTPClient struct {
	Host string
	Port int // 80 or 8888

	// CloseConnection sends "Connection: close" to disable keep-alive.
	CloseConnection bool

	client *http.Client
}

// NewHTTPClient returns a client for host:port.
func NewHTTPClient(host string, port int, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		Host: host,
		Port: port,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy: nil,
				// Must stay below the endpoint's 65s keep-alive timeout.
				IdleConnTimeout: 60 * time.Second,
			},
		},
	}
}

// HTTPRequest is one message to send.
type HTTPRequest struct {
	ContentType string // e.g. application/json, text/plain, application/octet-stream
	Body        []byte

	// Timestamp, if non-zero, is sent as x-soracom-timestamp (UNIX ms) so
	// Harvest Data stores the record at that time instead of the receive time.
	Timestamp time.Time
}

// Send POSTs the request to the endpoint root.
func (c *HTTPClient) Send(ctx context.Context, r HTTPRequest) (_ *Response, err error) {
	url := fmt.Sprintf("http://%s:%d/", c.Host, c.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(r.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", r.ContentType)
	if !r.Timestamp.IsZero() {
		req.Header.Set("x-soracom-timestamp", strconv.FormatInt(r.Timestamp.UnixMilli(), 10))
	}
	if c.CloseConnection {
		req.Close = true
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &Response{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        body,
	}, nil
}
