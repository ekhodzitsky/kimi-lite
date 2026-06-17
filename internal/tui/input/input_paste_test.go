package input

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
