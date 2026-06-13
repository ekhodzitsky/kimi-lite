package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ekhodzitsky/kimi-lite/internal/idgen"
	"github.com/ekhodzitsky/kimi-lite/pkg/api"
)

// mockStore is an in-memory implementation of api.Store for testing.
type mockStore struct {
	mu       sync.Mutex
	sessions map[string]*api.Session
	messages map[string][]api.Message
	turns    map[string][]api.Turn
	closed   bool
	serial   int64
}

func newMockStore() *mockStore {
	return &mockStore{
		sessions: make(map[string]*api.Session),
		messages: make(map[string][]api.Message),
		turns:    make(map[string][]api.Turn),
	}
}

func (m *mockStore) CreateSession(ctx context.Context, path string) (*api.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.serial++
	now := time.Now().UTC().Add(time.Duration(m.serial) * time.Microsecond)
	sess := &api.Session{
		ID:        idgen.GenerateID(),
		Path:      path,
		Messages:  []api.Message{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.sessions[sess.ID] = sess
	m.messages[sess.ID] = []api.Message{}
	m.turns[sess.ID] = []api.Turn{}
	return sess, nil
}

func (m *mockStore) GetSession(ctx context.Context, id string) (*api.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	copy := *sess
	copy.Messages = append([]api.Message(nil), m.messages[id]...)
	return &copy, nil
}

func (m *mockStore) GetLastSession(ctx context.Context, path string) (*api.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var latest *api.Session
	for _, sess := range m.sessions {
		if sess.Path == path {
			if latest == nil || sess.UpdatedAt.After(latest.UpdatedAt) ||
				(sess.UpdatedAt.Equal(latest.UpdatedAt) && sess.CreatedAt.After(latest.CreatedAt)) {
				latest = sess
			}
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("session not found")
	}
	copy := *latest
	copy.Messages = append([]api.Message(nil), m.messages[latest.ID]...)
	return &copy, nil
}

func (m *mockStore) ListSessions(ctx context.Context, path string, limit int) ([]api.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []api.Session
	for _, sess := range m.sessions {
		if sess.Path == path {
			copy := *sess
			copy.Messages = []api.Message{}
			list = append(list, copy)
		}
	}
	// Sort by UpdatedAt desc, then CreatedAt desc
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].UpdatedAt.After(list[i].UpdatedAt) ||
				(list[j].UpdatedAt.Equal(list[i].UpdatedAt) && list[j].CreatedAt.After(list[i].CreatedAt)) {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func (m *mockStore) UpdateSession(ctx context.Context, session *api.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[session.ID]; !ok {
		return fmt.Errorf("session not found")
	}
	session.UpdatedAt = time.Now().UTC()
	m.sessions[session.ID] = session
	return nil
}

func (m *mockStore) DeleteSession(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	delete(m.messages, id)
	delete(m.turns, id)
	return nil
}

func (m *mockStore) AppendMessage(ctx context.Context, sessionID string, msg api.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[sessionID] = append(m.messages[sessionID], msg)
	if sess, ok := m.sessions[sessionID]; ok {
		sess.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *mockStore) GetMessages(ctx context.Context, sessionID string, limit int) ([]api.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msgs := append([]api.Message(nil), m.messages[sessionID]...)
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[:limit]
	}
	return msgs, nil
}

func (m *mockStore) ClearMessages(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[sessionID] = []api.Message{}
	return nil
}

func (m *mockStore) ReplaceMessages(ctx context.Context, sessionID string, msgs []api.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages[sessionID] = append([]api.Message(nil), msgs...)
	return nil
}

func (m *mockStore) SaveTurn(ctx context.Context, sessionID string, turn api.Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	found := false
	for i, t := range m.turns[sessionID] {
		if t.ID == turn.ID {
			m.turns[sessionID][i] = turn
			found = true
			break
		}
	}
	if !found {
		m.turns[sessionID] = append(m.turns[sessionID], turn)
	}
	return nil
}

func (m *mockStore) GetTurns(ctx context.Context, sessionID string, limit int) ([]api.Turn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	turns := append([]api.Turn(nil), m.turns[sessionID]...)
	if limit > 0 && len(turns) > limit {
		turns = turns[:limit]
	}
	return turns, nil
}

func (m *mockStore) CountTurns(ctx context.Context, sessionID string, state api.TurnState) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, t := range m.turns[sessionID] {
		if t.State == state {
			count++
		}
	}
	return count, nil
}

func (m *mockStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// mockLLMClient is a test double for api.LLMClient.
type mockLLMClient struct {
	chatFunc       func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error)
	chatStreamFunc func(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error)
	models         []api.ModelInfo
}

func (m *mockLLMClient) Chat(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (*api.Message, error) {
	return m.chatFunc(ctx, messages, tools)
}

func (m *mockLLMClient) ChatStream(ctx context.Context, messages []api.Message, tools []api.ToolDefinition) (<-chan api.StreamChunk, error) {
	return m.chatStreamFunc(ctx, messages, tools)
}

func (m *mockLLMClient) Models() []api.ModelInfo {
	return m.models
}

// mockToolExecutor is a test double for api.ToolExecutor.
type mockToolExecutor struct {
	executeFunc func(ctx context.Context, call api.ToolCall) (api.ToolResult, error)
	defs        []api.ToolDefinition
}

func (m *mockToolExecutor) Execute(ctx context.Context, call api.ToolCall) (api.ToolResult, error) {
	return m.executeFunc(ctx, call)
}

func (m *mockToolExecutor) Definitions(ctx context.Context) []api.ToolDefinition {
	return m.defs
}

// mockApprovalGate is a test double for api.ApprovalGate.
type mockApprovalGate struct {
	shouldAutoApprove func(call api.ToolCall) (api.ApprovalDecision, bool)
}

func (m *mockApprovalGate) ShouldAutoApprove(call api.ToolCall) (api.ApprovalDecision, bool) {
	return m.shouldAutoApprove(call)
}

// mockConfigProvider is a test double for api.ConfigProvider.
type mockConfigProvider struct {
	cfg *api.Config
}

func (m *mockConfigProvider) Get() *api.Config {
	return m.cfg
}
