package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"council-of-reeds/internal/types"
)

// State holds the complete persistent council ledger
type State struct {
	Universes      map[string]*types.Passport       `json:"universes"`
	ActiveVariants map[string]*types.VariantSession `json:"active_variants"`
	Messages       []*types.Message                 `json:"messages"`
	UpdatedAt      time.Time                        `json:"updated_at"`
}

// Store handles persistent reads and writes
type Store struct {
	mu       sync.RWMutex
	filePath string
	state    State
}

// NewStore initializes or loads the local ledger
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", err)
	}

	filePath := filepath.Join(dataDir, "council_ledger.json")
	s := &Store{
		filePath: filePath,
		state: State{
			Universes:      make(map[string]*types.Passport),
			ActiveVariants: make(map[string]*types.VariantSession),
			Messages:       make([]*types.Message, 0),
		},
	}

	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load store: %w", err)
	}

	return s, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, &s.state); err != nil {
		return err
	}
	if s.state.Universes == nil {
		s.state.Universes = make(map[string]*types.Passport)
	}
	if s.state.ActiveVariants == nil {
		s.state.ActiveVariants = make(map[string]*types.VariantSession)
	}
	if s.state.Messages == nil {
		s.state.Messages = make([]*types.Message, 0)
	}
	return nil
}

func (s *Store) saveLocked() error {
	s.state.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}

	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpFile, s.filePath)
}

// UpsertVariantSession stores or updates an active variant session
func (s *Store) UpsertVariantSession(session *types.VariantSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.ActiveVariants[session.VariantTag] = session
	return s.saveLocked()
}

// DeleteVariantSession removes a retired or superseded variant session
func (s *Store) DeleteVariantSession(variantTag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.state.ActiveVariants, variantTag)
	return s.saveLocked()
}

// GetAllVariantSessions returns all persisted active variants
func (s *Store) GetAllVariantSessions() []*types.VariantSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]*types.VariantSession, 0, len(s.state.ActiveVariants))
	for _, v := range s.state.ActiveVariants {
		res = append(res, v)
	}
	return res
}

// UpsertPassport stores or updates a universe passport
func (s *Store) UpsertPassport(p *types.Passport) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	p.LastActive = time.Now().UTC()
	s.state.Universes[p.UniverseID] = p
	return s.saveLocked()
}

// GetPassport returns a universe passport if known
func (s *Store) GetPassport(universeID string) (*types.Passport, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, exists := s.state.Universes[universeID]
	return p, exists
}

// GetAllPassports returns all registered universes
func (s *Store) GetAllPassports() []*types.Passport {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]*types.Passport, 0, len(s.state.Universes))
	for _, p := range s.state.Universes {
		res = append(res, p)
	}
	return res
}

// AppendMessage records a message in the ledger
func (s *Store) AppendMessage(msg *types.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Messages = append(s.state.Messages, msg)
	return s.saveLocked()
}

// UpdateMessageStatus updates status (e.g. STALE, WITHDRAWN, DELIVERED)
func (s *Store) UpdateMessageStatus(msgID string, status types.MessageStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range s.state.Messages {
		if m.ID == msgID {
			m.Status = status
			return s.saveLocked()
		}
	}
	return fmt.Errorf("message not found: %s", msgID)
}

// GetRecentMessages retrieves the last N messages
func (s *Store) GetRecentMessages(limit int) []*types.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.state.Messages)
	if total == 0 {
		return []*types.Message{}
	}
	start := total - limit
	if start < 0 {
		start = 0
	}
	out := make([]*types.Message, total-start)
	copy(out, s.state.Messages[start:])
	return out
}

// GetPendingForUniverse retrieves active/pending messages aimed at this universe
func (s *Store) GetPendingForUniverse(universeID string) []*types.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*types.Message, 0)
	for _, m := range s.state.Messages {
		if (m.ToUniverse == universeID || m.ToUniverse == "@council" || m.ToUniverse == "") &&
			(m.Status == types.StatusPending || m.Status == types.StatusStale) {
			out = append(out, m)
		}
	}
	return out
}

