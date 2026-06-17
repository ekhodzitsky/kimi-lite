package llm

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestMessageContent_WithImageURL(t *testing.T) {
	t.Parallel()

	c := NewClient(api.LLMConfig{BaseURL: "http://localhost", APIKey: "key", Model: "m"}, nil)
	msg := api.Message{
		Role:    api.RoleUser,
		Content: "describe",
		ContentParts: []api.ContentPart{
			{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "https://example.com/img.png"}},
		},
	}

	got := c.messageContent(msg)
	parts, ok := got.([]contentPart)
	if !ok {
		t.Fatalf("expected []contentPart, got %T", got)
	}
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "describe" {
		t.Errorf("first part = %+v, want text 'describe'", parts[0])
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL.URL != "https://example.com/img.png" {
		t.Errorf("second part = %+v, want image_url", parts[1])
	}
}

func TestMessageContent_WithLocalImagePath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "dot.png")
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatalf("write test png: %v", err)
	}

	c := NewClient(api.LLMConfig{BaseURL: "http://localhost", APIKey: "key", Model: "m"}, nil)
	c.SetAttachmentRoots([]string{tmpDir})
	msg := api.Message{
		Role:    api.RoleUser,
		Content: "describe",
		ContentParts: []api.ContentPart{
			{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: path}},
		},
	}

	got := c.messageContent(msg)
	parts, ok := got.([]contentPart)
	if !ok {
		t.Fatalf("expected []contentPart, got %T", got)
	}
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}
	if parts[1].Type != "image_url" {
		t.Fatalf("second part type = %q, want image_url", parts[1].Type)
	}
	if !strings.HasPrefix(parts[1].ImageURL.URL, "data:image/png;base64,") {
		t.Errorf("URL = %q, want data URL", parts[1].ImageURL.URL)
	}
}

func TestMessageContent_WithImageData(t *testing.T) {
	t.Parallel()

	c := NewClient(api.LLMConfig{BaseURL: "http://localhost", APIKey: "key", Model: "m"}, nil)
	data := base64.StdEncoding.EncodeToString([]byte("fake-image"))
	msg := api.Message{
		Role:    api.RoleUser,
		Content: "look",
		ContentParts: []api.ContentPart{
			{Type: api.ContentPartImageData, ImageData: &api.ImageData{MIMEType: "image/png", Data: data}},
		},
	}

	got := c.messageContent(msg)
	parts, ok := got.([]contentPart)
	if !ok {
		t.Fatalf("expected []contentPart, got %T", got)
	}
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}
	if parts[1].Type != "image_url" {
		t.Fatalf("second part type = %q, want image_url", parts[1].Type)
	}
	wantURL := "data:image/png;base64," + data
	if parts[1].ImageURL.URL != wantURL {
		t.Errorf("URL = %q, want %q", parts[1].ImageURL.URL, wantURL)
	}
}

func TestMessageContent_NoParts(t *testing.T) {
	t.Parallel()

	c := NewClient(api.LLMConfig{BaseURL: "http://localhost", APIKey: "key", Model: "m"}, nil)
	msg := api.Message{Role: api.RoleUser, Content: "hello"}
	got := c.messageContent(msg)
	if s, ok := got.(string); !ok || s != "hello" {
		t.Errorf("messageContent = %v, want string hello", got)
	}
}

func TestBuildChatRequest_WithContentParts(t *testing.T) {
	t.Parallel()

	c := NewClient(api.LLMConfig{BaseURL: "http://localhost", APIKey: "key", Model: "m"}, nil)
	req := c.buildChatRequest([]api.Message{{
		Role:    api.RoleUser,
		Content: "describe",
		ContentParts: []api.ContentPart{
			{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "https://example.com/img.png"}},
		},
	}}, nil, false)

	if len(req.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(req.Messages))
	}
	b, err := json.Marshal(req.Messages[0].Content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	if !strings.Contains(string(b), `"type":"image_url"`) {
		t.Errorf("request content missing image_url part: %s", b)
	}
}

func TestImageURLToDataURL_RejectsPathOutsideRoots(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	otherDir := t.TempDir()
	path := filepath.Join(otherDir, "dot.png")
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatalf("write test png: %v", err)
	}

	c := NewClient(api.LLMConfig{BaseURL: "http://localhost", APIKey: "key", Model: "m"}, nil)
	c.SetAttachmentRoots([]string{tmpDir})

	got := c.imageURLToDataURL(path)
	if got != path {
		t.Errorf("URL = %q, want original path %q", got, path)
	}
}

func TestImageURLToDataURL_RejectsWithoutRoots(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "dot.png")
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52}
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatalf("write test png: %v", err)
	}

	c := NewClient(api.LLMConfig{BaseURL: "http://localhost", APIKey: "key", Model: "m"}, nil)
	// No roots configured.

	got := c.imageURLToDataURL(path)
	if got != path {
		t.Errorf("URL = %q, want original path %q", got, path)
	}
}

func TestImageURLToDataURL_SizeCap(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	c := NewClient(api.LLMConfig{BaseURL: "http://localhost", APIKey: "key", Model: "m"}, nil)
	c.SetAttachmentRoots([]string{tmpDir})

	path := filepath.Join(tmpDir, "huge.png")
	if err := os.WriteFile(path, make([]byte, maxAttachmentSize+1), 0o600); err != nil {
		t.Fatalf("write large file: %v", err)
	}

	got := c.imageURLToDataURL(path)
	if got != path {
		t.Errorf("URL = %q, want original path %q because file exceeds size cap", got, path)
	}
}
