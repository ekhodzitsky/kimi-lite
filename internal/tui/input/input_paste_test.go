package input

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/clipboard"
	"github.com/ekhodzitsky/kimi-lite/internal/tui/styles"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

func TestPasteMsgAddsAttachment(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 100)
	m.SetWidth(80)

	_, cmd := m.Update(PasteMsg{Parts: []api.ContentPart{
		{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "/tmp/paste.png"}},
	}})
	if cmd != nil {
		t.Error("PasteMsg should not produce a command")
	}

	atts := m.Attachments()
	if len(atts) != 1 {
		t.Fatalf("attachments = %d, want 1", len(atts))
	}
	if atts[0].Path != "/tmp/paste.png" {
		t.Errorf("attachment path = %q, want %q", atts[0].Path, "/tmp/paste.png")
	}
}

func TestPasteMsgAddsNonImageAttachment(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 100)
	m.SetWidth(80)

	_, cmd := m.Update(PasteMsg{Parts: []api.ContentPart{
		{Type: api.ContentPartText, Text: "[Attached file: /tmp/notes.txt]"},
	}})
	if cmd != nil {
		t.Error("PasteMsg should not produce a command")
	}

	atts := m.Attachments()
	if len(atts) != 1 {
		t.Fatalf("attachments = %d, want 1", len(atts))
	}
	if atts[0].Path != "/tmp/notes.txt" {
		t.Errorf("attachment path = %q, want %q", atts[0].Path, "/tmp/notes.txt")
	}
	if atts[0].MIMEType != "application/octet-stream" {
		t.Errorf("attachment mime = %q, want application/octet-stream", atts[0].MIMEType)
	}
}

func TestSendWithAttachments(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 100)
	m.SetWidth(80)

	m.addContentPart(api.ContentPart{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "/tmp/paste.png"}})
	m.SetValue("describe this")

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command after send")
	}

	msg, ok := cmd().(SendMsg)
	if !ok {
		t.Fatalf("expected SendMsg, got %T", cmd())
	}
	if msg.Content != "describe this" {
		t.Errorf("SendMsg.Content = %q, want %q", msg.Content, "describe this")
	}
	if len(msg.ContentParts) != 1 {
		t.Fatalf("SendMsg.ContentParts = %d, want 1", len(msg.ContentParts))
	}
	if msg.ContentParts[0].Type != api.ContentPartImageURL {
		t.Errorf("content part type = %q, want image_url", msg.ContentParts[0].Type)
	}

	if len(m.Attachments()) != 0 {
		t.Error("attachments should be cleared after send")
	}
}

func TestSendWithNonImageAttachment(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 100)
	m.SetWidth(80)

	m.addContentPart(api.ContentPart{Type: api.ContentPartText, Text: "[Attached file: /tmp/notes.txt]"})
	m.SetValue("see attached")

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command after send")
	}

	msg, ok := cmd().(SendMsg)
	if !ok {
		t.Fatalf("expected SendMsg, got %T", cmd())
	}
	if len(msg.ContentParts) != 1 {
		t.Fatalf("SendMsg.ContentParts = %d, want 1", len(msg.ContentParts))
	}
	if msg.ContentParts[0].Type != api.ContentPartText {
		t.Errorf("content part type = %q, want text", msg.ContentParts[0].Type)
	}
	if !strings.Contains(msg.ContentParts[0].Text, "/tmp/notes.txt") {
		t.Errorf("content part text = %q, want marker with path", msg.ContentParts[0].Text)
	}
}

func TestPasteErrorMsgSurfacesStatus(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 100)
	m.SetWidth(80)

	_, cmd := m.Update(PasteErrorMsg{Err: os.ErrNotExist})
	if cmd == nil {
		t.Fatal("expected a command after paste error")
	}

	msg := cmd()
	errMsg, ok := msg.(PasteErrorMsg)
	if !ok {
		t.Fatalf("expected PasteErrorMsg, got %T", msg)
	}
	if !strings.Contains(errMsg.Err.Error(), "paste failed") {
		t.Errorf("error = %q, want it to contain 'paste failed'", errMsg.Err.Error())
	}
}

func TestReadClipboardAttachmentsCopiesFileToTemp(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	src := filepath.Join(configDir, "source.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	origReadImage := readImageFn
	origReadFilePaths := readFilePathsFn
	origCopyFileToTemp := copyFileToTempFn
	defer func() {
		readImageFn = origReadImage
		readFilePathsFn = origReadFilePaths
		copyFileToTempFn = origCopyFileToTemp
	}()

	readImageFn = func(context.Context) ([]byte, string, error) {
		return nil, "", os.ErrNotExist
	}
	readFilePathsFn = func(context.Context) ([]string, error) {
		return []string{src}, nil
	}
	copyFileToTempFn = clipboard.CopyFileToTemp

	parts, err := readClipboardAttachments(t.Context(), configDir)
	if err != nil {
		t.Fatalf("readClipboardAttachments error: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(parts))
	}
	if parts[0].Type != api.ContentPartText {
		t.Errorf("part type = %q, want text", parts[0].Type)
	}
	if !strings.HasPrefix(parts[0].Text, "[Attached file: ") {
		t.Errorf("part text = %q, want attached-file marker", parts[0].Text)
	}
	if !strings.Contains(parts[0].Text, filepath.Join(configDir, "tmp")) {
		t.Errorf("part text = %q, want path under configDir/tmp", parts[0].Text)
	}
}

func TestPasteMsgPlainTextInsertsIntoValue(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 100)
	m.SetWidth(80)
	m.SetValue("before ")

	_, cmd := m.Update(PasteMsg{Parts: []api.ContentPart{
		{Type: api.ContentPartText, Text: "pasted text"},
	}})
	if cmd != nil {
		t.Error("PasteMsg should not produce a command")
	}

	if !strings.Contains(m.Value(), "pasted text") {
		t.Errorf("value = %q, should contain pasted text", m.Value())
	}
	if len(m.Attachments()) != 0 {
		t.Error("plain text paste should not create an empty-path attachment")
	}
}

func TestResetClearsAttachments(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 100)
	m.addContentPart(api.ContentPart{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "/tmp/paste.png"}})

	m.Reset()
	if len(m.Attachments()) != 0 {
		t.Error("Reset should clear attachments")
	}
}

func TestAttachmentViewRenders(t *testing.T) {
	t.Parallel()

	st := styles.New("dark")
	m := New(st, DefaultKeyMap(), 100)
	m.SetWidth(80)
	m.addContentPart(api.ContentPart{Type: api.ContentPartImageURL, ImageURL: &api.ImageURL{URL: "/tmp/paste.png"}})

	view := m.View().Content
	if view == "" {
		t.Error("View should render non-empty content")
	}
	if !strings.Contains(view, "paste.png") {
		t.Errorf("View should contain attachment name, got %q", view)
	}
}
