package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type Client struct {
	urls       []string
	httpClient *http.Client
	retries    int
	backoff    time.Duration
	nextID     uint64
	activeMu   sync.Mutex
	active     int
	lastURL    string
	observer   func(url string, calls int64)
}

type Option func(*Client)

func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

func WithRetries(retries int) Option {
	return func(c *Client) {
		c.retries = retries
	}
}

func WithCallObserver(observer func(url string, calls int64)) Option {
	return func(c *Client) {
		c.observer = observer
	}
}

func NewClient(urls []string, timeout time.Duration, opts ...Option) (*Client, error) {
	if len(urls) == 0 {
		return nil, errors.New("at least one RPC URL is required")
	}
	client := &Client{
		urls: urls,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		retries: 2,
		backoff: 500 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(client)
	}
	return client, nil
}

func (c *Client) ActiveURL() string {
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	if c.lastURL != "" {
		return c.lastURL
	}
	return c.urls[c.active]
}

func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	request := c.newRequest(method, params)
	var response responsePayload
	if err := c.doWithFallback(ctx, request, &response, 1); err != nil {
		return err
	}
	return json.Unmarshal(response.Result, result)
}

func (c *Client) BatchCall(ctx context.Context, calls []Call, results []any) error {
	if len(calls) != len(results) {
		return fmt.Errorf("calls and results length mismatch")
	}
	if len(calls) == 0 {
		return nil
	}

	requests := make([]requestPayload, 0, len(calls))
	for _, call := range calls {
		requests = append(requests, c.newRequest(call.Method, call.Params))
	}

	var responses []responsePayload
	if err := c.doWithFallback(ctx, requests, &responses, int64(len(calls))); err != nil {
		return err
	}
	if err := validateBatchResponses(responses); err != nil {
		return c.doWithFallback(ctx, requests, &responses, int64(len(calls)))
	}

	byID := make(map[uint64]responsePayload, len(responses))
	for _, response := range responses {
		byID[response.ID] = response
	}

	for index, request := range requests {
		response, ok := byID[request.ID]
		if !ok {
			return fmt.Errorf("missing response for RPC request %d", request.ID)
		}
		if response.Error != nil {
			return fmt.Errorf("rpc error: %s", response.Error.Message)
		}
		if err := json.Unmarshal(response.Result, results[index]); err != nil {
			return err
		}
	}
	return nil
}

type Call struct {
	Method string
	Params any
}

type requestPayload struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type responsePayload struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) newRequest(method string, params any) requestPayload {
	return requestPayload{
		JSONRPC: "2.0",
		ID:      atomic.AddUint64(&c.nextID, 1),
		Method:  method,
		Params:  params,
	}
}

func (c *Client) doWithFallback(ctx context.Context, payload any, result any, calls int64) error {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		start := c.nextStart()
		for offset := range c.urls {
			url := c.urls[(start+offset)%len(c.urls)]
			if err := c.post(ctx, url, payload, result); err == nil {
				c.markSuccess(url, calls)
				return nil
			} else {
				lastErr = err
			}
		}
		if attempt < c.retries {
			timer := time.NewTimer(c.backoff * time.Duration(attempt+1))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return fmt.Errorf("rpc request failed: %w", lastErr)
}

func (c *Client) post(ctx context.Context, url string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "contract-scraper/0.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d from %s: %s", resp.StatusCode, url, string(data))
	}

	if response, ok := result.(*responsePayload); ok {
		if err := json.Unmarshal(data, response); err != nil {
			return err
		}
		if response.Error != nil {
			return fmt.Errorf("rpc error: %s", response.Error.Message)
		}
		return nil
	}

	if err := json.Unmarshal(data, result); err != nil {
		return err
	}

	return nil
}

func validateBatchResponses(responses []responsePayload) error {
	for _, response := range responses {
		if response.Error != nil {
			return fmt.Errorf("rpc error: %s", response.Error.Message)
		}
	}
	return nil
}

func (c *Client) nextStart() int {
	c.activeMu.Lock()
	defer c.activeMu.Unlock()
	start := c.active
	c.active = (c.active + 1) % len(c.urls)
	return start
}

func (c *Client) markSuccess(url string, calls int64) {
	c.activeMu.Lock()
	c.lastURL = url
	observer := c.observer
	c.activeMu.Unlock()
	if observer != nil {
		observer(url, calls)
	}
}
