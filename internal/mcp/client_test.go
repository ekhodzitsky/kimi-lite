package mcp

import (
	"context"
	"encoding/json"
	"errors"
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

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
