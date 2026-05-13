package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
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

func TestCallUsesRPCURLsRoundRobin(t *testing.T) {
	var requested []string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requested = append(requested, r.URL.String())
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
		[]string{"https://a.test", "https://b.test", "https://c.test"},
		time.Second,
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	if err != nil {
		t.Fatal(err)
	}

	for range 5 {
		var latest string
		if err := client.Call(context.Background(), "eth_blockNumber", []any{}, &latest); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"https://a.test", "https://b.test", "https://c.test", "https://a.test", "https://b.test"}
	if !reflect.DeepEqual(requested, want) {
		t.Fatalf("requested = %#v, want %#v", requested, want)
	}
}

func TestCallObserverReceivesSuccessfulRPCURL(t *testing.T) {
	var observed []string
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
		[]string{"https://a.test", "https://b.test"},
		time.Second,
		WithHTTPClient(&http.Client{Transport: transport}),
		WithCallObserver(func(url string, calls int64) {
			for range calls {
				observed = append(observed, url)
			}
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	for range 3 {
		var latest string
		if err := client.Call(context.Background(), "eth_blockNumber", []any{}, &latest); err != nil {
			t.Fatal(err)
		}
	}

	want := []string{"https://a.test", "https://b.test", "https://a.test"}
	if !reflect.DeepEqual(observed, want) {
		t.Fatalf("observed = %#v, want %#v", observed, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
