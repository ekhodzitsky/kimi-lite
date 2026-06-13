package tui

import "strings"

// renderBuffer owns renderedContent and incremental-render bookkeeping.
//
// The stable prefix (everything before the currently-streaming assistant
// message) is kept as an immutable string so that each streaming chunk only
// re-renders the trailing active block instead of copying the entire prefix.
// Blocks appended while the active block is empty are stored in a slice and
// joined lazily in String(), making appendBlock O(1) amortized.
type renderBuffer struct {
	prefix                 string
	prefixHasSep           bool
	completed              []string
	active                 strings.Builder
	lastAssistantRenderPos int
	dirty                  bool
}

func newRenderBuffer() *renderBuffer {
	return &renderBuffer{}
}

// String returns the accumulated rendered content.
func (rb *renderBuffer) String() string {
	if len(rb.completed) == 0 && rb.active.Len() == 0 {
		return rb.prefix
	}
	var b strings.Builder
	b.WriteString(rb.prefix)
	if len(rb.completed) > 0 {
		if rb.prefix != "" && !rb.prefixHasSep {
			b.WriteString("\n\n")
		}
		for i, block := range rb.completed {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(block)
		}
	}
	b.WriteString(rb.active.String())
	return b.String()
}

// len returns the current byte length of the buffer.
func (rb *renderBuffer) len() int {
	n := len(rb.prefix)
	if len(rb.completed) > 0 {
		if rb.prefix != "" && !rb.prefixHasSep {
			n += 2
		}
		for _, block := range rb.completed {
			n += len(block)
		}
		n += 2 * (len(rb.completed) - 1)
	}
	n += rb.active.Len()
	return n
}

// appendBlock appends a rendered message block with a separator.
func (rb *renderBuffer) appendBlock(block string) {
	// Finalize the current active content into the last completed block so the
	// new block is appended after the current render.
	if rb.active.Len() > 0 {
		if len(rb.completed) > 0 {
			rb.completed[len(rb.completed)-1] += rb.active.String()
		} else {
			rb.prefix += rb.active.String()
			rb.prefixHasSep = strings.HasSuffix(rb.prefix, "\n\n")
		}
		rb.active.Reset()
	}
	rb.completed = append(rb.completed, block)
	rb.lastAssistantRenderPos = 0
	rb.dirty = true
}

// reset clears the buffer and resets bookkeeping.
func (rb *renderBuffer) reset() {
	rb.prefix = ""
	rb.prefixHasSep = false
	rb.completed = nil
	rb.active.Reset()
	rb.lastAssistantRenderPos = 0
	rb.dirty = true
}

// rebuild reconstructs the buffer from a slice of rendered blocks.
func (rb *renderBuffer) rebuild(blocks []string) {
	var b strings.Builder
	for i, block := range blocks {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(block)
	}
	rb.prefix = b.String()
	rb.prefixHasSep = false
	rb.completed = nil
	rb.active.Reset()
	rb.lastAssistantRenderPos = 0
	rb.dirty = true
}

// setLastBlockStart records the byte position where the current assistant block
// begins. When the start position is at the end of the existing content, a block
// separator is prepended so that incremental updates match the layout produced
// by rebuild.
func (rb *renderBuffer) setLastBlockStart(pos int) {
	full := rb.String()
	if pos < 0 {
		pos = 0
	}
	if pos > len(full) {
		pos = len(full)
	}

	rb.prefix = full[:pos]
	rb.prefixHasSep = strings.HasSuffix(rb.prefix, "\n\n")
	rb.completed = nil
	rb.active.Reset()
	if pos < len(full) {
		rb.active.WriteString(full[pos:])
	}

	// Starting a brand-new block at the end of the buffer needs a separator
	// to remain consistent with rebuild().
	if pos == len(full) && pos > 0 && !rb.prefixHasSep {
		rb.prefix += "\n\n"
		rb.prefixHasSep = true
		rb.lastAssistantRenderPos = pos + 2
	} else {
		rb.lastAssistantRenderPos = pos
	}
}

// updateLastBlock re-renders the trailing assistant block without touching the
// stable prefix.
func (rb *renderBuffer) updateLastBlock(view string) {
	rb.active.Reset()
	rb.active.WriteString(view)
	rb.dirty = true
}

// lastBlockStart returns the recorded byte position of the current assistant block.
func (rb *renderBuffer) lastBlockStart() int {
	return rb.lastAssistantRenderPos
}

// isDirty reports whether the buffer has changed since the last markClean.
func (rb *renderBuffer) isDirty() bool {
	return rb.dirty
}

// markClean clears the dirty flag.
func (rb *renderBuffer) markClean() {
	rb.dirty = false
}
