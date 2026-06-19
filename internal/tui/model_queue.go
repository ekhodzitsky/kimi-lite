package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/ekhodzitsky/kimi-lite/internal/tui/input"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// queuedMessage holds a user message that was drafted while the assistant was
// still streaming or thinking. Messages are sent in FIFO order once the turn
// returns to idle.
type queuedMessage struct {
	content      string
	contentParts []api.ContentPart
}

// enqueueMessage appends a drafted message to the queue and updates the input
// placeholder so the user sees how many messages are waiting.
func (m *Model) enqueueMessage(content string, parts []api.ContentPart) {
	m.mu.Lock()
	m.messageQueue = append(m.messageQueue, queuedMessage{
		content:      content,
		contentParts: parts,
	})
	m.mu.Unlock()
	m.input.SetQueueCount(len(m.messageQueue))
}

// queueLength returns the number of queued messages.
func (m *Model) queueLength() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.messageQueue)
}

// sendNextQueuedCmd returns a command that emits an input.SendMsg for the next
// queued message, or nil if the queue is empty. The message is dequeued when
// the command is created so that only one auto-send is ever in flight for a
// given idle transition.
func (m *Model) sendNextQueuedCmd() tea.Cmd {
	m.mu.Lock()
	if len(m.messageQueue) == 0 {
		m.mu.Unlock()
		return nil
	}
	qm := m.messageQueue[0]
	m.messageQueue = m.messageQueue[1:]
	count := len(m.messageQueue)
	m.mu.Unlock()

	m.input.SetQueueCount(count)
	return func() tea.Msg {
		return input.SendMsg{Content: qm.content, ContentParts: qm.contentParts}
	}
}
