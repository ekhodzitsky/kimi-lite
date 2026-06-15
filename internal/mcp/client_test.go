package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// mockTransport is a test double for Transport.
type mockTransport struct {
	connectFunc func(ctx context.Context) error
	sendFunc    func(ctx context.Context, method string, params any) (*JSONRPCResponse, error)
	notifyFunc  func(ctx context.Context, method string, params any) error
	closeFunc   func() error
}

func (m *mockTransport) Connect(ctx context.Context) error {
	if m.connectFunc != nil {
		return m.connectFunc(ctx)
	}
	return nil
}

func (m *mockTransport) Send(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	if m.sendFunc != nil {
		return m.sendFunc(ctx, method, params)
	}
	return &JSONRPCResponse{}, nil
}

func (m *mockTransport) Notify(ctx context.Context, method string, params any) error {
	if m.notifyFunc != nil {
		return m.notifyFunc(ctx, method, params)
	}
	return nil
}

func (m *mockTransport) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func TestClient_Connect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		transport *mockTransport
		wantErr   bool
		errMsg    string
	}{
		{
			name: "success",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					if method == "initialize" {
						return &JSONRPCResponse{
							Result: mustMarshal(t, map[string]any{
								"protocolVersion": "2024-11-05",
							}),
						}, nil
					}
					return &JSONRPCResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "transport connect fails",
			transport: &mockTransport{
				connectFunc: func(ctx context.Context) error {
					return errors.New("connection refused")
				},
			},
			wantErr: true,
			errMsg:  "connection refused",
		},
		{
			name: "initialize fails",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return nil, errors.New("initialize timeout")
				},
			},
			wantErr: true,
			errMsg:  "initialize timeout",
		},
		{
			name: "invalid initialize response",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return &JSONRPCResponse{
						Result: json.RawMessage(`{invalid}`),
					}, nil
				},
			},
			wantErr: true,
			errMsg:  "unmarshal initialize response",
		},
		{
			name: "initialized notification fails",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					if method == "initialize" {
						return &JSONRPCResponse{
							Result: mustMarshal(t, map[string]any{
								"protocolVersion": "2024-11-05",
							}),
						}, nil
					}
					return &JSONRPCResponse{}, nil
				},
				notifyFunc: func(ctx context.Context, method string, params any) error {
					return errors.New("notification failed")
				},
			},
			wantErr: true,
			errMsg:  "notification failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := NewClient(tt.transport)
			err := client.Connect(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestClient_ListTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		transport *mockTransport
		want      []api.ToolDefinition
		wantErr   bool
		errMsg    string
	}{
		{
			name: "success",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return &JSONRPCResponse{
						Result: mustMarshal(t, map[string]any{
							"tools": []api.ToolDefinition{
								{Name: "read_file", Description: "Read a file"},
							},
						}),
					}, nil
				},
			},
			want: []api.ToolDefinition{
				{Name: "read_file", Description: "Read a file"},
			},
		},
		{
			name: "transport error",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return nil, errors.New("network error")
				},
			},
			wantErr: true,
			errMsg:  "network error",
		},
		{
			name: "invalid response",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return &JSONRPCResponse{
						Result: json.RawMessage(`{invalid}`),
					}, nil
				},
			},
			wantErr: true,
			errMsg:  "unmarshal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := NewClient(tt.transport)
			got, err := client.ListTools(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d tools, got %d", len(tt.want), len(got))
			}
			for i := range got {
				if got[i].Name != tt.want[i].Name {
					t.Fatalf("expected tool name %q, got %q", tt.want[i].Name, got[i].Name)
				}
			}
		})
	}
}

func TestClient_CallTool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		toolName  string
		args      map[string]any
		transport *mockTransport
		want      string
		wantErr   bool
		errMsg    string
	}{
		{
			name:     "success",
			toolName: "read_file",
			args:     map[string]any{"path": "/tmp/test"},
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return &JSONRPCResponse{
						Result: mustMarshal(t, map[string]any{
							"content": []map[string]string{
								{"type": "text", "text": "hello world"},
							},
							"isError": false,
						}),
					}, nil
				},
			},
			want: "hello world",
		},
		{
			name:     "tool returns error",
			toolName: "shell",
			args:     map[string]any{"command": "false"},
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return &JSONRPCResponse{
						Result: mustMarshal(t, map[string]any{
							"content": []map[string]string{
								{"type": "text", "text": "exit status 1"},
							},
							"isError": true,
						}),
					}, nil
				},
			},
			wantErr: true,
			errMsg:  "exit status 1",
		},
		{
			name:     "transport error",
			toolName: "read_file",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return nil, errors.New("disconnected")
				},
			},
			wantErr: true,
			errMsg:  "disconnected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := NewClient(tt.transport)
			got, err := client.CallTool(context.Background(), tt.toolName, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestClient_Close(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		transport *mockTransport
		wantErr   bool
	}{
		{
			name:      "success",
			transport: &mockTransport{},
			wantErr:   false,
		},
		{
			name: "transport close error",
			transport: &mockTransport{
				closeFunc: func() error {
					return errors.New("already closed")
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := NewClient(tt.transport)
			err := client.Close()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	t.Parallel()

	transport := &mockTransport{
		sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	client := NewClient(transport)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.ListTools(ctx)
	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestClient_CallTool_NonTextContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		transport *mockTransport
		want      string
		wantErr   bool
		errMsg    string
	}{
		{
			name: "multiple text blocks joined with newlines",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return &JSONRPCResponse{
						Result: mustMarshal(t, map[string]any{
							"content": []map[string]string{
								{"type": "text", "text": "first line"},
								{"type": "text", "text": "second line"},
							},
							"isError": false,
						}),
					}, nil
				},
			},
			want: "first line\nsecond line",
		},
		{
			name: "non-text block produces placeholder",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return &JSONRPCResponse{
						Result: mustMarshal(t, map[string]any{
							"content": []map[string]any{
								{"type": "text", "text": "hello"},
								{"type": "image"},
							},
							"isError": false,
						}),
					}, nil
				},
			},
			want: "hello\n[image]",
		},
		{
			name: "resource block with uri",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					return &JSONRPCResponse{
						Result: mustMarshal(t, map[string]any{
							"content": []map[string]any{
								{"type": "resource", "resource": map[string]string{"uri": "file:///tmp/res.txt"}},
							},
							"isError": false,
						}),
					}, nil
				},
			},
			want: "[resource: file:///tmp/res.txt]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := NewClient(tt.transport)
			got, err := client.CallTool(context.Background(), "test", nil)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestClient_CallTool_EmptyError(t *testing.T) {
	t.Parallel()

	transport := &mockTransport{
		sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
			return &JSONRPCResponse{
				Result: mustMarshal(t, map[string]any{
					"content": []map[string]string{},
					"isError": true,
				}),
			}, nil
		},
	}

	client := NewClient(transport)
	_, err := client.CallTool(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tool returned an error with no text content") {
		t.Fatalf("expected generic error message, got: %v", err)
	}
}

func TestClient_CallTool_MalformedContent(t *testing.T) {
	t.Parallel()

	transport := &mockTransport{
		sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
			return &JSONRPCResponse{
				Result: mustMarshal(t, map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "valid"},
						{"type": "text", "text": 123},
						{"type": "text", "text": "after"},
					},
					"isError": false,
				}),
			}, nil
		},
	}

	client := NewClient(transport)
	got, err := client.CallTool(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "valid\n[malformed content item]\nafter"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestClient_Connect_ProtocolVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		transport *mockTransport
		wantErr   bool
		errMsg    string
	}{
		{
			name: "empty protocol version",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					if method == "initialize" {
						return &JSONRPCResponse{
							Result: mustMarshal(t, map[string]any{
								"protocolVersion": "",
							}),
						}, nil
					}
					return &JSONRPCResponse{}, nil
				},
			},
			wantErr: true,
			errMsg:  "empty protocol version",
		},
		{
			name: "incompatible protocol version",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					if method == "initialize" {
						return &JSONRPCResponse{
							Result: mustMarshal(t, map[string]any{
								"protocolVersion": "2025-01-01",
							}),
						}, nil
					}
					return &JSONRPCResponse{}, nil
				},
			},
			wantErr: true,
			errMsg:  "unsupported protocol version",
		},
		{
			name: "valid protocol version",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					if method == "initialize" {
						return &JSONRPCResponse{
							Result: mustMarshal(t, map[string]any{
								"protocolVersion": "2024-11-05",
							}),
						}, nil
					}
					return &JSONRPCResponse{}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "older protocol version accepted",
			transport: &mockTransport{
				sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
					if method == "initialize" {
						return &JSONRPCResponse{
							Result: mustMarshal(t, map[string]any{
								"protocolVersion": "2024-10-01",
							}),
						}, nil
					}
					return &JSONRPCResponse{}, nil
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := NewClient(tt.transport)
			err := client.Connect(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestClient_Connect_ProtocolVersionCloseError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("close failed")
	transport := &mockTransport{
		sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
			if method == "initialize" {
				return &JSONRPCResponse{
					Result: mustMarshal(t, map[string]any{
						"protocolVersion": "",
					}),
				}, nil
			}
			return &JSONRPCResponse{}, nil
		},
		closeFunc: func() error { return closeErr },
	}

	client := NewClient(transport)
	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty protocol version") {
		t.Fatalf("expected empty protocol version error, got: %v", err)
	}
}

func TestNewClientFromServerConfig_Stdio(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
read line
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}'
read line
`

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "helper.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write helper script: %v", err)
	}

	cfg := api.MCPServerConfig{
		Transport: api.MCPTransportStdio,
		Command:   "/bin/sh",
		Args:      []string{scriptPath},
		Env:       map[string]string{"MCP_TEST_VAR": "value"},
		CWD:       tmpDir,
	}

	client, err := NewClientFromServerConfig(cfg, nil)
	if err != nil {
		t.Fatalf("NewClientFromServerConfig error: %v", err)
	}
	if client == nil {
		t.Fatal("expected client")
	}

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	_ = client.Close()
}

func TestNewClientFromServerConfig_HTTP(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "initialize" {
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  json.RawMessage(`{"protocolVersion":"2024-11-05"}`),
			})
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	cfg := api.MCPServerConfig{
		Transport:         api.MCPTransportHTTP,
		URL:               server.URL,
		Headers:           map[string]string{"X-Test": "yes"},
		BearerTokenEnvVar: "",
	}

	client, err := NewClientFromServerConfig(cfg, server.Client())
	if err != nil {
		t.Fatalf("NewClientFromServerConfig error: %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	_ = client.Close()
}

func TestNewClientFromServerConfig_UnsupportedTransport(t *testing.T) {
	t.Parallel()

	cfg := api.MCPServerConfig{Transport: "smtp"}
	_, err := NewClientFromServerConfig(cfg, nil)
	if err == nil {
		t.Fatal("expected error for unsupported transport")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported transport error, got: %v", err)
	}
}

func TestClient_Connect_CloseErrorLogs(t *testing.T) {
	// Not parallel because it replaces the default logger.
	handler := &testLogHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(old)

	transport := &mockTransport{
		sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
			return nil, errors.New("initialize failed")
		},
		closeFunc: func() error {
			return errors.New("close also failed")
		},
	}

	client := NewClient(transport)
	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	found := false
	for _, r := range handler.records {
		if r.Message == "failed to close mcp transport after initialize error" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected warning log for close error")
	}
}

func TestClient_Connect_DecodeErrorCloseError(t *testing.T) {
	handler := &testLogHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(old)

	transport := &mockTransport{
		sendFunc: func(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
			return &JSONRPCResponse{Result: json.RawMessage(`{invalid}`)}, nil
		},
		closeFunc: func() error {
			return errors.New("close failed")
		},
	}

	client := NewClient(transport)
	err := client.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	found := false
	for _, r := range handler.records {
		if strings.Contains(r.Message, "close mcp transport after initialize response decode error") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected warning log for close error after decode")
	}
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
