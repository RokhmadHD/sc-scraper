package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestCallUnwrapsJSONRPCResult(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var request requestPayload
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		response := responsePayload{
			JSONRPC: "2.0",
			ID:      request.ID,
			Result:  json.RawMessage(`"0xa"`),
		}
		var body bytes.Buffer
		if err := json.NewEncoder(&body).Encode(response); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(&body),
			Header:     make(http.Header),
		}, nil
	})

	client, err := NewClient(
		[]string{"https://example.test"},
		time.Second,
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	if err != nil {
		t.Fatal(err)
	}

	var latest string
	if err := client.Call(context.Background(), "eth_blockNumber", []any{}, &latest); err != nil {
		t.Fatal(err)
	}
	if latest != "0xa" {
		t.Fatalf("latest = %s, want 0xa", latest)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
