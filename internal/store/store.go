package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"council-of-reeds/internal/types"
)

// State holds the complete persistent council ledger
type State struct {
	Universes      map[string]*types.Passport       `json:"universes"`
	ActiveVariants map[string]*types.VariantSession `json:"active_variants"`
	VariantHistory map[string]*types.VariantSession `json:"variant_history,omitempty"`
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
			VariantHistory: make(map[string]*types.VariantSession),
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
	if s.state.VariantHistory == nil {
		s.state.VariantHistory = make(map[string]*types.VariantSession)
	}
	if s.state.Messages == nil {
		s.state.Messages = make([]*types.Message, 0)
	}

	// Conservative legacy migration
	now := time.Now().UTC()
	for _, m := range s.state.Messages {
		if m.TTLMinutes <= 0 {
			m.TTLMinutes = 20
		}
		deadline := m.CreatedAt.Add(time.Duration(m.TTLMinutes) * time.Minute)

		if m.QuestionState == "" {
			if m.Status == types.StatusAnswered {
				m.QuestionState = types.QuestionAnswered
			} else if m.Status == types.StatusWithdrawn {
				m.QuestionState = types.QuestionWithdrawn
			} else if !now.Before(deadline) { // now >= deadline -> expired
				m.QuestionState = types.QuestionExpired
			} else if m.Type == types.MsgConsultation {
				if m.Status == types.StatusPending {
					m.QuestionState = types.QuestionOpen
				} else {
					m.QuestionState = types.QuestionNone
				}
			} else {
				m.QuestionState = types.QuestionNone
			}
		}

		// Ensure recipients structure is populated
		if len(m.Recipients) == 0 && m.ToUniverse != "" {
			if strings.HasPrefix(m.ToUniverse, "@") {
				tag := strings.TrimPrefix(m.ToUniverse, "@")
				m.Recipients = []types.Recipient{{VariantTag: tag}}
				if m.OriginalTarget == "" {
					m.OriginalTarget = "@" + tag
				}
			} else {
				m.Recipients = []types.Recipient{{UniverseID: m.ToUniverse}}
				if m.OriginalTarget == "" {
					m.OriginalTarget = m.ToUniverse
				}
			}
		}
	}

	for _, v := range s.state.ActiveVariants {
		if v.State == "" {
			v.State = types.VariantActive
		}
	}
	for _, v := range s.state.VariantHistory {
		if v.State == "" {
			v.State = types.VariantSuperseded
		}
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

	// Try atomic rename first
	if err := os.Rename(tmpFile, s.filePath); err != nil {
		// On Windows, if destination exists or sandbox blocks atomic replace, retry or fallback
		_ = os.Remove(s.filePath)
		if err := os.Rename(tmpFile, s.filePath); err != nil {
			// Direct write fallback
			if err := os.WriteFile(s.filePath, data, 0644); err != nil {
				return err
			}
			_ = os.Remove(tmpFile)
		}
	}
	return nil
}

// UpsertVariantSession stores or updates an active variant session
func (s *Store) UpsertVariantSession(session *types.VariantSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.ActiveVariants[session.VariantTag] = session
	return s.saveLocked()
}

// ArchiveSupersededVariant transitions a predecessor to SUPERSEDED, sets its successor pointer, and preserves its snapshot
func (s *Store) ArchiveSupersededVariant(predTag string, successorTag string, histPassport *types.Passport, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	v, exists := s.state.ActiveVariants[predTag]
	if !exists {
		v, exists = s.state.VariantHistory[predTag]
	}
	if !exists {
		return fmt.Errorf("predecessor variant not found: %s", predTag)
	}

	v.State = types.VariantSuperseded
	v.SuccessorTag = successorTag
	v.HandoverTime = &now
	if histPassport != nil {
		pCopy := *histPassport
		v.HistoricalPassport = &pCopy
	}

	s.state.VariantHistory[predTag] = v
	delete(s.state.ActiveVariants, predTag)
	return s.saveLocked()
}

// GetVariantSession looks up a variant in active variants or historical variants (case-insensitive)
func (s *Store) GetVariantSession(variantTag string) (*types.VariantSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleaned := strings.TrimPrefix(strings.TrimSpace(variantTag), "@")
	for k, v := range s.state.ActiveVariants {
		if strings.EqualFold(k, cleaned) {
			return v, true
		}
	}
	for k, v := range s.state.VariantHistory {
		if strings.EqualFold(k, cleaned) {
			return v, true
		}
	}
	return nil, false
}

// GetPredecessorLineage returns the slice of ancestor tags (immediate predecessor first) with cycle detection
func (s *Store) GetPredecessorLineage(variantTag string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getPredecessorLineageLocked(variantTag)
}

func (s *Store) getPredecessorLineageLocked(variantTag string) ([]string, error) {
	cleaned := strings.TrimPrefix(strings.TrimSpace(variantTag), "@")
	var lineage []string
	visited := make(map[string]bool)
	curr := cleaned

	for {
		visited[strings.ToLower(curr)] = true
		var predTag string

		for k, v := range s.state.ActiveVariants {
			if strings.EqualFold(k, curr) && v != nil && v.PredecessorTag != "" {
				predTag = strings.TrimPrefix(v.PredecessorTag, "@")
				break
			}
		}
		if predTag == "" {
			for k, v := range s.state.VariantHistory {
				if strings.EqualFold(k, curr) && v != nil && v.PredecessorTag != "" {
					predTag = strings.TrimPrefix(v.PredecessorTag, "@")
					break
				}
			}
		}

		if predTag == "" {
			break
		}
		if visited[strings.ToLower(predTag)] {
			return lineage, fmt.Errorf("cycle detected in predecessor lineage at variant @%s", predTag)
		}
		lineage = append(lineage, predTag)
		curr = predTag
	}

	return lineage, nil
}

// GetTransitiveSuccessor resolves an old handle through successor pointers to the current leaf variant
func (s *Store) GetTransitiveSuccessor(variantTag string) (*types.VariantSession, []string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleaned := strings.TrimPrefix(strings.TrimSpace(variantTag), "@")
	var hops []string
	visited := make(map[string]bool)
	curr := cleaned

	for {
		visited[strings.ToLower(curr)] = true
		var v *types.VariantSession
		exists := false

		for k, val := range s.state.ActiveVariants {
			if strings.EqualFold(k, curr) {
				v = val
				exists = true
				break
			}
		}
		if !exists {
			for k, val := range s.state.VariantHistory {
				if strings.EqualFold(k, curr) {
					v = val
					exists = true
					break
				}
			}
		}
		if !exists {
			return nil, hops, fmt.Errorf("variant not found: %s", curr)
		}

		if v.State != types.VariantSuperseded || v.SuccessorTag == "" {
			return v, hops, nil
		}

		succ := strings.TrimPrefix(v.SuccessorTag, "@")
		if visited[strings.ToLower(succ)] {
			return nil, hops, fmt.Errorf("cycle detected in successor lineage at variant @%s", succ)
		}
		hops = append(hops, succ)
		curr = succ
	}
}

// DeleteVariantSession removes a variant session from active variants
func (s *Store) DeleteVariantSession(variantTag string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleaned := strings.TrimPrefix(strings.TrimSpace(variantTag), "@")
	delete(s.state.ActiveVariants, cleaned)
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

// IsOpenQuestion evaluates whether a message is an open question strictly within its TTL deadline
func IsOpenQuestion(m *types.Message, now time.Time) bool {
	if m.QuestionState != types.QuestionOpen {
		return false
	}
	ttl := m.TTLMinutes
	if ttl <= 0 {
		ttl = 20
	}
	deadline := m.CreatedAt.Add(time.Duration(ttl) * time.Minute)
	// Open strictly while now < deadline (now >= deadline means expired)
	return now.Before(deadline)
}

// GetOpenQuestionsForVariant retrieves open questions addressing this universe broadcast, this variant, or its declared predecessors
func (s *Store) GetOpenQuestionsForVariant(universeID string, variantTag string, predecessorTags []string, now time.Time) []*types.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	predSet := make(map[string]bool)
	for _, p := range predecessorTags {
		predSet[strings.ToLower(strings.TrimPrefix(p, "@"))] = true
	}
	myTagClean := strings.ToLower(strings.TrimPrefix(variantTag, "@"))

	out := make([]*types.Message, 0)
	dirty := false

	for _, m := range s.state.Messages {
		if m.QuestionState == types.QuestionOpen {
			ttl := m.TTLMinutes
			if ttl <= 0 {
				ttl = 20
			}
			deadline := m.CreatedAt.Add(time.Duration(ttl) * time.Minute)
			if !now.Before(deadline) {
				m.QuestionState = types.QuestionExpired
				m.Status = types.StatusStale
				dirty = true
				continue
			}

			// Check if targeted to this variant, an authorized predecessor, or general universe broadcast
			matched := false

			// 1. Check Recipients list
			if len(m.Recipients) > 0 {
				for _, r := range m.Recipients {
					rTag := strings.ToLower(strings.TrimPrefix(r.VariantTag, "@"))
					if rTag != "" {
						if rTag == myTagClean || predSet[rTag] {
							matched = true
							break
						}
					} else if r.UniverseID != "" && strings.EqualFold(r.UniverseID, universeID) {
						// Universe-wide question
						matched = true
						break
					}
				}
			}

			// 2. Fallback checks on legacy / top-level fields
			if !matched {
				origClean := strings.ToLower(strings.TrimPrefix(m.OriginalTarget, "@"))
				actualClean := strings.ToLower(strings.TrimPrefix(m.ActualRecipient, "@"))
				toClean := strings.ToLower(strings.TrimPrefix(m.ToUniverse, "@"))

				if origClean == myTagClean || predSet[origClean] ||
					actualClean == myTagClean || predSet[actualClean] ||
					toClean == myTagClean || predSet[toClean] {
					matched = true
				} else if (m.ToUniverse == universeID || m.ToUniverse == "@council" || m.ToUniverse == "") &&
					origClean == "" && actualClean == "" && !strings.HasPrefix(m.ToUniverse, "@") {
					// Universe-wide broadcast
					matched = true
				}
			}

			if matched {
				out = append(out, m)
			}
		}
	}

	if dirty {
		_ = s.saveLocked()
	}
	return out
}

// CloseQuestionWithReply validates in_reply_to, checks responder authorization, and closes the question atomically
func (s *Store) CloseQuestionWithReply(replyMsg *types.Message, now time.Time) (*types.Message, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var targetQ *types.Message
	for _, m := range s.state.Messages {
		if m.ID == replyMsg.InReplyTo {
			targetQ = m
			break
		}
	}

	if targetQ == nil {
		return nil, false, fmt.Errorf("referenced in_reply_to question not found: %s", replyMsg.InReplyTo)
	}

	// Check deadline
	ttl := targetQ.TTLMinutes
	if ttl <= 0 {
		ttl = 20
	}
	deadline := targetQ.CreatedAt.Add(time.Duration(ttl) * time.Minute)

	if targetQ.QuestionState == types.QuestionOpen && !now.Before(deadline) {
		targetQ.QuestionState = types.QuestionExpired
		targetQ.Status = types.StatusStale
	}

	// Check if already closed
	if targetQ.QuestionState == types.QuestionAnswered {
		// Late informative reply: permitted as history, do not reopen or double-close
		replyMsg.QuestionState = types.QuestionNone
		replyMsg.Status = types.StatusDelivered
		s.state.Messages = append(s.state.Messages, replyMsg)
		_ = s.saveLocked()
		return targetQ, false, nil
	}

	if targetQ.QuestionState == types.QuestionWithdrawn {
		return nil, false, fmt.Errorf("question %s was already withdrawn by sender", targetQ.ID)
	}
	if targetQ.QuestionState == types.QuestionExpired {
		return nil, false, fmt.Errorf("question %s has expired (deadline exceeded)", targetQ.ID)
	}
	if targetQ.QuestionState != types.QuestionOpen {
		return nil, false, fmt.Errorf("message %s is not an open question (state: %s)", targetQ.ID, targetQ.QuestionState)
	}

	// Validate responder authorization
	responderVariant := strings.ToLower(strings.TrimPrefix(replyMsg.FromVariant, "@"))
	responderUniverse := strings.ToLower(replyMsg.FromUniverse)
	authorized := false

	if targetQ.ToUniverse == "@council" || targetQ.ToUniverse == "" || strings.EqualFold(targetQ.ToUniverse, responderUniverse) {
		authorized = true
	}
	if !authorized && len(targetQ.Recipients) > 0 {
		for _, r := range targetQ.Recipients {
			rTag := strings.ToLower(strings.TrimPrefix(r.VariantTag, "@"))
			rUniv := strings.ToLower(r.UniverseID)
			if rTag != "" && (rTag == responderVariant) {
				authorized = true
				break
			}
			if rUniv != "" && rUniv == responderUniverse {
				authorized = true
				break
			}
		}
	}
	if !authorized {
		orig := strings.ToLower(strings.TrimPrefix(targetQ.OriginalTarget, "@"))
		act := strings.ToLower(strings.TrimPrefix(targetQ.ActualRecipient, "@"))
		if orig == responderVariant || act == responderVariant {
			authorized = true
		}
	}

	// Check if responder is a declared successor of any recipient
	if !authorized && responderVariant != "" {
		preds, _ := s.getPredecessorLineageLocked(responderVariant)
		for _, pred := range preds {
			predLower := strings.ToLower(pred)
			for _, r := range targetQ.Recipients {
				rTag := strings.ToLower(strings.TrimPrefix(r.VariantTag, "@"))
				if rTag == predLower {
					authorized = true
					break
				}
			}
			orig := strings.ToLower(strings.TrimPrefix(targetQ.OriginalTarget, "@"))
			act := strings.ToLower(strings.TrimPrefix(targetQ.ActualRecipient, "@"))
			to := strings.ToLower(strings.TrimPrefix(targetQ.ToUniverse, "@"))
			if orig == predLower || act == predLower || to == predLower {
				authorized = true
				break
			}
			if authorized {
				break
			}
		}
	}

	if !authorized {
		return nil, false, fmt.Errorf("responder @%s is not an authorized recipient or successor for question %s", replyMsg.FromVariant, targetQ.ID)
	}

	// Atomically close question and record reply
	targetQ.QuestionState = types.QuestionAnswered
	targetQ.Status = types.StatusAnswered
	targetQ.AnsweredBy = replyMsg.FromVariant
	targetQ.AnsweredAt = &now

	replyMsg.QuestionState = types.QuestionNone
	replyMsg.Status = types.StatusDelivered
	s.state.Messages = append(s.state.Messages, replyMsg)

	if err := s.saveLocked(); err != nil {
		return nil, false, err
	}
	return targetQ, true, nil
}

// WithdrawQuestion withdraws an open question by ID
func (s *Store) WithdrawQuestion(msgID string, fromVariant string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range s.state.Messages {
		if m.ID == msgID {
			ttl := m.TTLMinutes
			if ttl <= 0 {
				ttl = 20
			}
			deadline := m.CreatedAt.Add(time.Duration(ttl) * time.Minute)
			if !now.Before(deadline) {
				m.QuestionState = types.QuestionExpired
				m.Status = types.StatusStale
				_ = s.saveLocked()
				return fmt.Errorf("cannot withdraw question %s: already expired by deadline", msgID)
			}
			m.QuestionState = types.QuestionWithdrawn
			m.Status = types.StatusWithdrawn
			return s.saveLocked()
		}
	}
	return fmt.Errorf("message not found: %s", msgID)
}

// SweepExpiredQuestions marks all open questions whose deadline has passed as EXPIRED
func (s *Store) SweepExpiredQuestions(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, m := range s.state.Messages {
		if m.QuestionState == types.QuestionOpen {
			ttl := m.TTLMinutes
			if ttl <= 0 {
				ttl = 20
			}
			deadline := m.CreatedAt.Add(time.Duration(ttl) * time.Minute)
			if !now.Before(deadline) {
				m.QuestionState = types.QuestionExpired
				m.Status = types.StatusStale
				count++
			}
		}
	}
	if count > 0 {
		_ = s.saveLocked()
	}
	return count
}

