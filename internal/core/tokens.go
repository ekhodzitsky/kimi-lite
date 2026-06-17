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
//
// Limitations: this is a coarse heuristic. It does not use a real tokenizer,
// does not model per-role formatting overhead beyond a fixed per-message cost,
// and treats all non-ASCII runes as a single token. Use it for budgeting and
// ordering, not for exact billing or context limits.
func (e *HeuristicTokenEstimator) Estimate(messages []api.Message) int {
	tokens := 0
	for _, m := range messages {
		tokens += e.perMessageOverhead
		tokens += e.estimateString(m.Content)
		for _, p := range m.ContentParts {
			tokens += e.estimateContentPart(p)
		}
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

// estimateContentPart returns a rough token budget for a content part.
// Text is estimated like any other string; images use a fixed heuristic.
func (e *HeuristicTokenEstimator) estimateContentPart(p api.ContentPart) int {
	switch p.Type {
	case api.ContentPartImageURL:
		// OpenAI charges 85 base tokens plus 170 per 512x512 tile for low-res,
		// and ~1105 tokens for high-res auto detail. Use a middle estimate.
		if p.ImageURL != nil && p.ImageURL.Detail == "low" {
			return 85
		}
		return 255
	case api.ContentPartImageData:
		return 255
	default:
		return e.estimateString(p.Text)
	}
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
