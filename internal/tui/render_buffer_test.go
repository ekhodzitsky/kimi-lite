package tui

import (
	"strings"
	"testing"
)

func TestRenderBuffer_AppendBlock(t *testing.T) {
	t.Parallel()

	rb := newRenderBuffer()

	rb.appendBlock("first")
	if got := rb.String(); got != "first" {
		t.Errorf("String() = %q, want %q", got, "first")
	}
	if rb.lastBlockStart() != 0 {
		t.Errorf("lastBlockStart() = %d, want 0", rb.lastBlockStart())
	}

	rb.appendBlock("second")
	want := "first\n\nsecond"
	if got := rb.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if rb.lastBlockStart() != 0 {
		t.Errorf("lastBlockStart() after append = %d, want 0", rb.lastBlockStart())
	}
}

func TestRenderBuffer_Reset(t *testing.T) {
	t.Parallel()

	rb := newRenderBuffer()
	rb.appendBlock("block")
	rb.updateLastBlock("extra")

	rb.reset()
	if got := rb.String(); got != "" {
		t.Errorf("String() after reset = %q, want empty", got)
	}
	if rb.lastBlockStart() != 0 {
		t.Errorf("lastBlockStart() after reset = %d, want 0", rb.lastBlockStart())
	}
	if rb.len() != 0 {
		t.Errorf("len() after reset = %d, want 0", rb.len())
	}
}

func TestRenderBuffer_Rebuild(t *testing.T) {
	t.Parallel()

	rb := newRenderBuffer()
	rb.appendBlock("old")

	rb.rebuild([]string{"a", "b", "c"})
	want := "a\n\nb\n\nc"
	if got := rb.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if rb.lastBlockStart() != 0 {
		t.Errorf("lastBlockStart() after rebuild = %d, want 0", rb.lastBlockStart())
	}
}

func TestRenderBuffer_SetLastBlockStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		blocks    []string
		pos       int
		wantStr   string
		wantStart int
	}{
		{
			name:      "split inside existing content",
			blocks:    []string{"prefix", "active"},
			pos:       len("prefix"),
			wantStr:   "prefix\n\nactive",
			wantStart: len("prefix"),
		},
		{
			name:      "split at end starts new block with separator",
			blocks:    []string{"prefix", "active"},
			pos:       len("prefix\n\nactive"),
			wantStr:   "prefix\n\nactive\n\n",
			wantStart: len("prefix\n\nactive") + 2,
		},
		{
			name:      "negative position clamps to zero",
			blocks:    []string{"only"},
			pos:       -5,
			wantStr:   "only",
			wantStart: 0,
		},
		{
			name:      "position beyond length clamps to end",
			blocks:    []string{"only"},
			pos:       100,
			wantStr:   "only\n\n",
			wantStart: len("only") + 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := newRenderBuffer()
			rb.rebuild(tt.blocks)
			rb.setLastBlockStart(tt.pos)

			if got := rb.String(); got != tt.wantStr {
				t.Errorf("String() = %q, want %q", got, tt.wantStr)
			}
			if got := rb.lastBlockStart(); got != tt.wantStart {
				t.Errorf("lastBlockStart() = %d, want %d", got, tt.wantStart)
			}
		})
	}
}

func TestRenderBuffer_UpdateLastBlock(t *testing.T) {
	t.Parallel()

	rb := newRenderBuffer()
	rb.rebuild([]string{"prefix", "active"})
	rb.setLastBlockStart(len("prefix"))

	rb.updateLastBlock("updated")
	want := "prefixupdated"
	if got := rb.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	rb.updateLastBlock("again")
	want = "prefixagain"
	if got := rb.String(); got != want {
		t.Errorf("String() after second update = %q, want %q", got, want)
	}
}

func TestRenderBuffer_DirtyFlag(t *testing.T) {
	t.Parallel()

	rb := newRenderBuffer()
	if rb.isDirty() {
		t.Error("new buffer should not be dirty")
	}

	rb.appendBlock("block")
	if !rb.isDirty() {
		t.Error("buffer should be dirty after appendBlock")
	}

	rb.markClean()
	if rb.isDirty() {
		t.Error("buffer should be clean after markClean")
	}

	rb.updateLastBlock("x")
	if !rb.isDirty() {
		t.Error("buffer should be dirty after updateLastBlock")
	}
}

func TestRenderBuffer_Len(t *testing.T) {
	t.Parallel()

	rb := newRenderBuffer()
	if rb.len() != 0 {
		t.Errorf("len() = %d, want 0", rb.len())
	}

	rb.appendBlock("hello")
	if rb.len() != len("hello") {
		t.Errorf("len() = %d, want %d", rb.len(), len("hello"))
	}

	rb.updateLastBlock(" world")
	want := len("hello") + len(" world")
	if rb.len() != want {
		t.Errorf("len() = %d, want %d", rb.len(), want)
	}
}

func TestRenderBuffer_LargeRebuildDoesNotOverflow(t *testing.T) {
	t.Parallel()

	rb := newRenderBuffer()
	blocks := make([]string, 100)
	for i := range blocks {
		blocks[i] = strings.Repeat("x", 100)
	}

	rb.rebuild(blocks)
	got := rb.String()
	wantLen := 100*100 + 99*len("\n\n")
	if len(got) != wantLen {
		t.Errorf("rebuilt length = %d, want %d", len(got), wantLen)
	}
}
