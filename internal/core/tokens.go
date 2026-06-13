package core

import (
	"unicode/utf8"

	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// HeuristicTokenEstimator estimates token counts using a mixed heuristic:
//   - ASCII characters are counted at 4 characters per token (common for GPT
//     tokenizers on English/code).
//   - Non-ASCII runes (CJK, emoji, accented characters, etc.) are counted as
//     one token each.
//   - Tool calls add a fixed overhead plus token estimates for their names and
//     JSON arguments.
//   - Each message carries a small framing overhead.
//
// It is fast, allocation-light, and does not depend on external tokenizers.
type HeuristicTokenEstimator struct {
	perMessageOverhead int
	toolCallOverhead   int
}

// NewHeuristicTokenEstimator creates a token estimator with sensible defaults.
func NewHeuristicTokenEstimator() *HeuristicTokenEstimator {
	return &HeuristicTokenEstimator{
		perMessageOverhead: 3,
		toolCallOverhead:   10,
	}
}

// Estimate returns the estimated token count for the provided messages.
func (e *HeuristicTokenEstimator) Estimate(messages []api.Message) int {
	tokens := 0
	for _, m := range messages {
		tokens += e.perMessageOverhead
		tokens += e.estimateString(m.Content)
		for _, tc := range m.ToolCalls {
			tokens += e.toolCallOverhead
			tokens += e.estimateString(tc.Name)
			tokens += e.estimateString(tc.Arguments)
		}
		if m.ToolCallID != "" {
			tokens += e.toolCallOverhead / 2
			tokens += e.estimateString(m.ToolCallID)
		}
	}
	return tokens
}

// estimateString returns a token estimate for a single string.
func (e *HeuristicTokenEstimator) estimateString(s string) int {
	asciiBytes := 0
	nonASCIIRunes := 0
	for i := 0; i < len(s); {
		b := s[i]
		if b < utf8.RuneSelf {
			asciiBytes++
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != utf8.RuneError {
			nonASCIIRunes++
			i += size
		} else {
			// Invalid UTF-8 byte: count as one non-ASCII token and advance.
			nonASCIIRunes++
			i++
		}
	}
	return asciiBytes/4 + nonASCIIRunes
}
