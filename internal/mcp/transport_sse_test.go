package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSETransport_RoundTrip(t *testing.T) {
	t.Parallel()

	type respJob struct {
		id int64
	}

	respCh := make(chan respJob, 4)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			f, _ := w.(http.Flusher)

			fmt.Fprintf(w, "event: endpoint\ndata: /message\n\n")
			if f != nil {
				f.Flush()
			}

			for {
				select {
				case <-r.Context().Done():
					return
				case job := <-respCh:
					fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"ok\":true}}\n\n", job.id)
					if f != nil {
						f.Flush()
					}
				}
			}
		case "/message":
			body, _ := io.ReadAll(r.Body)
			var req JSONRPCRequest
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			respCh <- respJob{id: req.ID}
		}
	}))
	defer srv.Close()

	tr := NewSSETransport(srv.URL+"/sse", nil, "", srv.Client())
	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	resp, err := tr.Send(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if resp.Result == nil {
		t.Fatal("expected result")
	}
}

func TestSSETransport_Notify(t *testing.T) {
	t.Parallel()

	notifyCalled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "event: endpoint\ndata: /notify\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done()
		case "/notify":
			notifyCalled <- struct{}{}
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	tr := NewSSETransport(srv.URL+"/sse", nil, "", srv.Client())
	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	if err := tr.Notify(ctx, "notification", map[string]string{"key": "value"}); err != nil {
		t.Fatalf("notify: %v", err)
	}

	select {
	case <-notifyCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("notify was not received")
	}
}

func TestSSETransport_ResolveEndpoint(t *testing.T) {
	t.Parallel()

	cases := []struct {
		base     string
		endpoint string
		want     string
	}{
		{"http://example.com/sse", "/message", "http://example.com/message"},
		{"http://example.com/sse", "http://other.com/message", "http://other.com/message"},
		{"http://example.com/path/sse", "message", "http://example.com/path/message"},
	}

	for _, tc := range cases {
		got, err := resolveReference(tc.base, tc.endpoint)
		if err != nil {
			t.Fatalf("resolveReference(%q, %q): %v", tc.base, tc.endpoint, err)
		}
		if got != tc.want {
			t.Errorf("resolveReference(%q, %q) = %q, want %q", tc.base, tc.endpoint, got, tc.want)
		}
	}
}

func TestSSETransport_ReadSSEEvent(t *testing.T) {
	t.Parallel()

	input := "event: message\ndata: {\"jsonrpc\":\"2.0\"}\n\n"
	reader := bufio.NewReader(strings.NewReader(input))
	ev, err := readSSEEvent(reader)
	if err != nil {
		t.Fatalf("readSSEEvent: %v", err)
	}
	if ev.event != "message" {
		t.Errorf("event = %q, want message", ev.event)
	}
	if ev.data != `{"jsonrpc":"2.0"}` {
		t.Errorf("data = %q, want json", ev.data)
	}
}
