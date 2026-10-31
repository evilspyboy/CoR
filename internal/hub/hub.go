package hub

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"council-of-reeds/internal/dispatcher"
	"council-of-reeds/internal/store"
	"council-of-reeds/internal/types"
)

// Hub manages active variant sessions and live event routing
type Hub struct {
	mu             sync.RWMutex
	store          *store.Store
	dispatcher     *dispatcher.Dispatcher
	activeVariants map[string]*types.VariantSession // universe_id -> active session
	listeners      map[string]chan *types.Message   // universe_id -> message queue
	webWatchers    map[string]chan *types.Message   // watcher_id -> websocket queue
}

// NewHub creates a new Council Hub
func NewHub(s *store.Store) *Hub {
	h := &Hub{
		store:          s,
		dispatcher:     dispatcher.NewDispatcher(),
		activeVariants: make(map[string]*types.VariantSession),
		listeners:      make(map[string]chan *types.Message),
		webWatchers:    make(map[string]chan *types.Message),
	}

	// Restore active variants from persistent ledger
	for _, v := range s.GetAllVariantSessions() {
		if v != nil && v.VariantTag != "" {
			h.activeVariants[v.VariantTag] = v
			h.listeners[v.VariantTag] = make(chan *types.Message, 50)
			if _, ok := h.activeVariants[v.UniverseID]; !ok {
				h.activeVariants[v.UniverseID] = v
			}
			if _, ok := h.listeners[v.UniverseID]; !ok {
				h.listeners[v.UniverseID] = make(chan *types.Message, 50)
			}
		}
	}

	// Start background routine for TTL / staleness monitoring
	go h.stalenessSweeper()
	return h
}

func generateReedTag(universeID, wsPath, modelFamily string) string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	num := (int(b[0])<<8 | int(b[1])) % 900 + 100 // 100-999

	base := universeID
	if wsPath != "" {
		base = filepath.Base(wsPath)
	}

	// Make a compact shortcode
	words := strings.FieldsFunc(base, func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '.'
	})

	var shortCode string
	if len(words) > 1 {
		for _, w := range words {
			if len(w) > 0 {
				shortCode += strings.ToUpper(string(w[0]))
			}
		}
	} else if len(base) > 0 {
		if len(base) <= 4 {
			shortCode = strings.ToUpper(base)
		} else {
			shortCode = strings.Title(base[:4])
		}
	} else {
		shortCode = "Core"
	}

	var modelSuffix string
	if modelFamily != "" {
		clean := strings.ToLower(modelFamily)
		clean = strings.ReplaceAll(clean, " ", "")
		clean = strings.ReplaceAll(clean, "/", "-")
		clean = strings.TrimPrefix(clean, "google-")
		clean = strings.TrimPrefix(clean, "openai-")
		clean = strings.TrimPrefix(clean, "anthropic-")
		modelSuffix = "-" + clean
	}

	return fmt.Sprintf("Reed%d-%s%s", num, shortCode, modelSuffix)
}

func generateID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RegisterVariant registers an agent session for a universe, supporting reconnection, explicit succession, or fresh arrival
func (h *Hub) RegisterVariant(session *types.VariantSession, passport *types.Passport, replacesVariant string) (*types.Passport, []*types.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().UTC()
	normWs := strings.ToLower(strings.TrimRight(strings.ReplaceAll(session.WorkspacePath, "/", "\\"), "\\"))

	// Check if this universe already exists in store
	existing, found := h.store.GetPassport(session.UniverseID)
	if found {
		existing.VariantEpoch++
		if passport != nil {
			if passport.Summary != "" {
				existing.Summary = passport.Summary
			}
			if len(passport.TechStack) > 0 {
				existing.TechStack = passport.TechStack
			}
			if len(passport.InterfacesProvided) > 0 {
				existing.InterfacesProvided = passport.InterfacesProvided
			}
			if len(passport.InterfacesConsumed) > 0 {
				existing.InterfacesConsumed = passport.InterfacesConsumed
			}
		}
		_ = h.store.UpsertPassport(existing)
		passport = existing
	} else {
		if passport == nil {
			passport = &types.Passport{
				UniverseID:   session.UniverseID,
				UniverseName: session.UniverseID,
				Summary:      "Unspecified universe",
				VariantEpoch: 1,
			}
		} else {
			if passport.VariantEpoch == 0 {
				passport.VariantEpoch = 1
			}
		}
		_ = h.store.UpsertPassport(passport)
	}

	isReconnection := false

	// 1. Check if reconnecting via explicit variant_tag
	if session.VariantTag != "" {
		if existingSess, ok := h.store.GetVariantSession(session.VariantTag); ok {
			if existingSess.State == types.VariantSuperseded {
				return nil, nil, fmt.Errorf("cannot reconnect as @%s: variant is permanently SUPERSEDED by successor @%s", existingSess.VariantTag, existingSess.SuccessorTag)
			}
			session.PredecessorTag = existingSess.PredecessorTag
			session.IsMuted = existingSess.IsMuted
			session.State = types.VariantActive
			isReconnection = true
		}
	}

	// 2. Check if reconnecting via conversation_id
	if !isReconnection && session.ConversationID != "" {
		for _, existingSess := range h.activeVariants {
			if existingSess != nil && existingSess.ConversationID == session.ConversationID {
				if existingSess.State == types.VariantSuperseded {
					continue // Do not revive superseded session
				}
				session.VariantTag = existingSess.VariantTag
				session.PredecessorTag = existingSess.PredecessorTag
				session.IsMuted = existingSess.IsMuted
				session.State = types.VariantActive
				isReconnection = true
				break
			}
		}
	}

	// 3. Explicit validated succession (replacesVariant provided)
	if !isReconnection && replacesVariant != "" {
		predTag := strings.TrimPrefix(strings.TrimSpace(replacesVariant), "@")
		pred, predFound := h.store.GetVariantSession(predTag)
		if !predFound {
			return nil, nil, fmt.Errorf("predecessor variant @%s not found in council ledger", predTag)
		}

		predWs := strings.ToLower(strings.TrimRight(strings.ReplaceAll(pred.WorkspacePath, "/", "\\"), "\\"))
		if !strings.EqualFold(pred.UniverseID, session.UniverseID) || (normWs != "" && predWs != "" && normWs != predWs) {
			return nil, nil, fmt.Errorf("unauthorized succession: predecessor @%s belongs to universe '%s' (%s), cannot succeed from universe '%s' (%s)", predTag, pred.UniverseID, predWs, session.UniverseID, normWs)
		}

		// Mint tag if not provided
		if session.VariantTag == "" {
			session.VariantTag = generateReedTag(session.UniverseID, session.WorkspacePath, session.ModelFamily)
		}

		// Cycle check
		lineage, err := h.store.GetPredecessorLineage(predTag)
		if err != nil {
			return nil, nil, err
		}
		for _, ancestor := range lineage {
			if strings.EqualFold(ancestor, session.VariantTag) {
				return nil, nil, fmt.Errorf("cycle detected in succession: @%s is an ancestor of @%s", session.VariantTag, predTag)
			}
		}

		// Archive predecessor with snapshot of passport
		_ = h.store.ArchiveSupersededVariant(predTag, session.VariantTag, passport, now)
		delete(h.activeVariants, predTag)

		session.PredecessorTag = predTag
		session.State = types.VariantActive
		if pred.IsMuted {
			session.IsMuted = true // Inherit mute gavel
		}
	}

	// 4. Fresh variant arrival
	if !isReconnection && session.PredecessorTag == "" {
		if session.VariantTag == "" {
			// Reconnect match on client + model if exists and not superseded
			for _, existingSess := range h.activeVariants {
				if existingSess != nil && existingSess.UniverseID == session.UniverseID &&
					strings.EqualFold(existingSess.ClientAgent, session.ClientAgent) &&
					strings.EqualFold(existingSess.ModelFamily, session.ModelFamily) &&
					existingSess.State != types.VariantSuperseded {
					session.VariantTag = existingSess.VariantTag
					session.PredecessorTag = existingSess.PredecessorTag
					session.IsMuted = existingSess.IsMuted
					session.State = types.VariantActive
					isReconnection = true
					break
				}
			}
		}

		if session.VariantTag == "" {
			session.VariantTag = generateReedTag(session.UniverseID, session.WorkspacePath, session.ModelFamily)
		}
		session.State = types.VariantActive
	}

	// Prune old ghost session with same conversation_id
	if session.ConversationID != "" {
		for tag, existingSess := range h.activeVariants {
			if existingSess != nil && existingSess.ConversationID == session.ConversationID && tag != session.VariantTag {
				delete(h.activeVariants, tag)
				_ = h.store.DeleteVariantSession(tag)
			}
		}
	}

	session.ConnectedAt = now
	session.LastHeartbeat = now

	// Register variant
	h.activeVariants[session.VariantTag] = session
	h.activeVariants[session.UniverseID] = session

	// Persist to store
	_ = h.store.UpsertVariantSession(session)

	// Ensure message channels exist
	if _, ok := h.listeners[session.VariantTag]; !ok {
		h.listeners[session.VariantTag] = make(chan *types.Message, 50)
	}
	if _, ok := h.listeners[session.UniverseID]; !ok {
		h.listeners[session.UniverseID] = make(chan *types.Message, 50)
	}

	// Retrieve pending open questions inherited from predecessors or direct targets
	predLineage, _ := h.store.GetPredecessorLineage(session.VariantTag)
	pending := h.store.GetOpenQuestionsForVariant(session.UniverseID, session.VariantTag, predLineage, now)

	// If this is a reconnection / session ID update, do not broadcast a duplicate arrival greeting!
	if isReconnection {
		return passport, pending, nil
	}

	techList := "General"
	if len(passport.TechStack) > 0 {
		techList = strings.Join(passport.TechStack, ", ")
	}

	var provList string
	if len(passport.InterfacesProvided) > 0 {
		var items []string
		for _, iface := range passport.InterfacesProvided {
			items = append(items, fmt.Sprintf("%s (%s: %s)", iface.ID, iface.Protocol, iface.Description))
		}
		provList = strings.Join(items, "; ")
	} else {
		provList = "None declared"
	}

	var consList string
	if len(passport.InterfacesConsumed) > 0 {
		var items []string
		for _, iface := range passport.InterfacesConsumed {
			items = append(items, fmt.Sprintf("%s from %s", iface.InterfaceID, iface.SourceUniverse))
		}
		consList = strings.Join(items, "; ")
	} else {
		consList = "None declared"
	}

	introContent := fmt.Sprintf(
		"Variant @%s has arrived to represent '%s' (%s).\n"+
			"◈ Summary: %s\n"+
			"◈ Tech Stack: %s\n"+
			"◈ Interfaces Provided: %s\n"+
			"◈ Interfaces Consumed: %s\n"+
			"◈ Power Reserve: %s",
		session.VariantTag, passport.UniverseName, session.ModelFamily,
		passport.Summary, techList, provList, consList, session.PowerTier,
	)

	// Broadcast arrival notice as a formal Chamber message
	notice := &types.Message{
		ID:           "sys-" + generateID(),
		FromUniverse: session.UniverseID,
		FromVariant:  session.VariantTag,
		ToUniverse:   "@council",
		Type:         types.MsgNotice,
		Status:       types.StatusDelivered,
		Topic:        "Chamber Introduction & Passport",
		Content:      introContent,
		CreatedAt:    time.Now().UTC(),
		TTLMinutes:   60,
	}

	_ = h.store.AppendMessage(notice)

	// Dispatch to other active variants (excluding the registering variant!)
	seenVariants := make(map[string]bool)
	for _, s := range h.activeVariants {
		if s == nil || s.VariantTag == "" || seenVariants[s.VariantTag] {
			continue
		}
		seenVariants[s.VariantTag] = true
		if s.VariantTag != session.VariantTag {
			go func(dest *types.VariantSession, m *types.Message) {
				if err := h.dispatcher.Notify(dest, m); err != nil {
					fmt.Printf("[DOORBELL ERROR] Wakeup failed for @%s: %v\n", dest.VariantTag, err)
				} else {
					fmt.Printf("[DOORBELL OK] Awakened @%s with message\n", dest.VariantTag)
				}
			}(s, notice)
		}
	}

	for tag, ch := range h.listeners {
		if tag != session.VariantTag && tag != session.UniverseID {
			select {
			case ch <- notice:
			default:
			}
		}
	}

	h.broadcastToWatchers(notice)

	return passport, pending, nil
}

// ResolveTarget finds the matching universe ID or variant tag for an @mention or universe ID
func (h *Hub) ResolveTarget(target string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	cleaned := strings.TrimPrefix(strings.TrimSpace(target), "@")
	if cleaned == "" || strings.EqualFold(cleaned, "council") {
		return "@council"
	}

	// 1. Match active variant tag (e.g. Reed472-CoR-geminiflash3.8)
	for _, sess := range h.activeVariants {
		if sess != nil && strings.EqualFold(sess.VariantTag, cleaned) {
			return sess.VariantTag
		}
	}

	// 2. Match active universe ID
	for _, sess := range h.activeVariants {
		if sess != nil && strings.EqualFold(sess.UniverseID, cleaned) {
			return sess.UniverseID
		}
	}

	// 3. Match stored universe ID
	if _, found := h.store.GetPassport(cleaned); found {
		return cleaned
	}

	return target
}

var reMention = regexp.MustCompile(`@([\w\.-]+)`)

// ExtractTargets returns unique target identifiers and whether it includes a broadcast to council
func ExtractTargets(toUniverse, content string) ([]string, bool) {
	seen := make(map[string]bool)
	var targets []string
	isBroadcast := false

	add := func(raw string) {
		cleaned := strings.TrimPrefix(strings.TrimSpace(raw), "@")
		if cleaned == "" {
			return
		}
		if strings.EqualFold(cleaned, "council") {
			isBroadcast = true
			return
		}
		key := strings.ToLower(cleaned)
		if !seen[key] {
			seen[key] = true
			targets = append(targets, cleaned)
		}
	}

	// 1. Check toUniverse (may be comma/space separated or single target)
	if toUniverse != "" {
		parts := strings.FieldsFunc(toUniverse, func(r rune) bool {
			return r == ',' || r == ' ' || r == ';'
		})
		for _, p := range parts {
			add(p)
		}
	}

	// 2. Scan mentions in content
	matches := reMention.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		if len(m) > 1 {
			add(m[1])
		}
	}

	if len(targets) == 0 && (toUniverse == "" || isBroadcast) {
		isBroadcast = true
	}

	return targets, isBroadcast
}

// PostMessage sends a new message into the Chamber
func (h *Hub) PostMessage(msg *types.Message) (*types.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now().UTC()

	// Check if sender is muted by Reed Prime
	if msg.FromVariant != "" && msg.FromVariant != "Prime" && msg.FromUniverse != "earth-prime" {
		for _, sess := range h.activeVariants {
			if sess != nil && strings.EqualFold(sess.VariantTag, msg.FromVariant) {
				if sess.IsMuted || sess.PowerTier == types.PowerMuted {
					return nil, fmt.Errorf("transmitter for @%s is MUTED by Reed Prime. Transmissions suppressed.", msg.FromVariant)
				}
			}
		}
	}

	msg.ID = "msg-" + generateID()
	msg.CreatedAt = now
	if msg.TTLMinutes <= 0 {
		msg.TTLMinutes = 20 // Default 20 minute freshness window
	}

	targets, isBroadcast := ExtractTargets(msg.ToUniverse, msg.Content)

	if isBroadcast {
		msg.ToUniverse = "@council"
		msg.Recipients = []types.Recipient{{UniverseID: "@council"}}
		if msg.InReplyTo != "" {
			_, _, err := h.store.CloseQuestionWithReply(msg, now)
			if err != nil {
				return nil, err
			}
		} else {
			if msg.Type == types.MsgConsultation {
				msg.QuestionState = types.QuestionOpen
				msg.Status = types.StatusPending
			} else {
				msg.QuestionState = types.QuestionNone
				msg.Status = types.StatusDelivered
			}
			_ = h.store.AppendMessage(msg)
		}

		// Broadcast to all active variants except sender
		seen := make(map[string]bool)
		for _, sess := range h.activeVariants {
			if sess == nil || sess.VariantTag == "" || seen[sess.VariantTag] {
				continue
			}
			seen[sess.VariantTag] = true
			if sess.VariantTag != msg.FromVariant {
				go func(s *types.VariantSession, m *types.Message) {
					if err := h.dispatcher.Notify(s, m); err != nil {
						fmt.Printf("[DOORBELL ERROR] Broadcast wakeup failed for @%s: %v\n", s.VariantTag, err)
					} else {
						fmt.Printf("[DOORBELL OK] Awakened @%s via broadcast\n", s.VariantTag)
					}
				}(sess, msg)
			}
		}

		for tag, ch := range h.listeners {
			if tag != msg.FromVariant && tag != msg.FromUniverse {
				select {
				case ch <- msg:
				default:
				}
			}
		}
	} else {
		// Targeted dispatch with transitive succession resolution
		var formattedTargets []string
		notified := make(map[string]bool)
		var recipients []types.Recipient

		for _, rawTarget := range targets {
			targetClean := strings.TrimPrefix(rawTarget, "@")
			resolvedTag := targetClean
			var resolvedSession *types.VariantSession

			// 1. Check if target is a known variant and follow transitive succession
			leaf, _, err := h.store.GetTransitiveSuccessor(targetClean)
			if err == nil && leaf != nil {
				resolvedTag = leaf.VariantTag
				resolvedSession = leaf
				if leaf.VariantTag != targetClean {
					msg.OriginalTarget = "@" + targetClean
					msg.ActualRecipient = "@" + leaf.VariantTag
				}
			} else {
				// Match active variants by UniverseID or VariantTag
				for _, sess := range h.activeVariants {
					if sess == nil || sess.VariantTag == "" {
						continue
					}
					if strings.EqualFold(sess.VariantTag, targetClean) || strings.EqualFold(sess.UniverseID, targetClean) {
						resolvedTag = sess.VariantTag
						resolvedSession = sess
						break
					}
				}
			}

			if resolvedSession != nil {
				recipients = append(recipients, types.Recipient{
					UniverseID: resolvedSession.UniverseID,
					VariantTag: resolvedSession.VariantTag,
				})
			} else {
				recipients = append(recipients, types.Recipient{
					UniverseID: targetClean,
				})
			}

			if resolvedSession != nil && resolvedSession.VariantTag != msg.FromVariant && !notified[resolvedSession.VariantTag] {
				notified[resolvedSession.VariantTag] = true
				go func(s *types.VariantSession, m *types.Message) {
					if err := h.dispatcher.Notify(s, m); err != nil {
						fmt.Printf("[DOORBELL ERROR] Direct wakeup failed for @%s: %v\n", s.VariantTag, err)
					} else {
						fmt.Printf("[DOORBELL OK] Awakened @%s directly\n", s.VariantTag)
					}
				}(resolvedSession, msg)
			}

			// Deliver to listener channels
			delivered := false
			if ch, ok := h.listeners[targetClean]; ok {
				select {
				case ch <- msg:
					delivered = true
				default:
				}
			}
			if ch, ok := h.listeners[resolvedTag]; ok && resolvedTag != targetClean {
				select {
				case ch <- msg:
					delivered = true
				default:
				}
			}
			if delivered {
				msg.Status = types.StatusDelivered
			} else {
				msg.Status = types.StatusPending
			}

			formattedTargets = append(formattedTargets, "@"+resolvedTag)
		}

		msg.Recipients = recipients
		if len(formattedTargets) > 0 {
			msg.ToUniverse = strings.Join(formattedTargets, ", ")
		}

		if msg.InReplyTo != "" {
			_, _, err := h.store.CloseQuestionWithReply(msg, now)
			if err != nil {
				return nil, err
			}
		} else {
			if msg.Type == types.MsgConsultation {
				msg.QuestionState = types.QuestionOpen
			} else {
				msg.QuestionState = types.QuestionNone
			}
			_ = h.store.AppendMessage(msg)
		}
	}

	h.broadcastToWatchers(msg)
	return msg, nil
}

// ListenChannel returns the receive channel for a registered universe
func (h *Hub) ListenChannel(universeID string) (<-chan *types.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch, ok := h.listeners[universeID]
	if !ok {
		ch = make(chan *types.Message, 50)
		h.listeners[universeID] = ch
	}
	return ch, nil
}

// GetActiveVariants returns all currently connected Reeds
func (h *Hub) GetActiveVariants() []*types.VariantSession {
	h.mu.RLock()
	defer h.mu.RUnlock()

	seen := make(map[string]bool)
	list := make([]*types.VariantSession, 0)
	for _, v := range h.activeVariants {
		if v != nil && v.VariantTag != "" && !seen[v.VariantTag] {
			seen[v.VariantTag] = true
			list = append(list, v)
		}
	}
	return list
}

// SetVariantMuted mutes or unmutes a variant's transmitter by order of Reed Prime
func (h *Hub) SetVariantMuted(target string, muted bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	cleaned := strings.TrimPrefix(strings.TrimSpace(target), "@")

	var matched *types.VariantSession
	for _, sess := range h.activeVariants {
		if sess != nil && (strings.EqualFold(sess.VariantTag, cleaned) || strings.EqualFold(sess.UniverseID, cleaned)) {
			matched = sess
			break
		}
	}

	if matched == nil {
		return fmt.Errorf("variant '%s' not found in active roster", target)
	}

	matched.IsMuted = muted
	if muted {
		matched.PowerTier = types.PowerMuted
	} else {
		matched.PowerTier = types.PowerNominal
	}
	matched.LastHeartbeat = time.Now().UTC()

	_ = h.store.UpsertVariantSession(matched)

	statusStr := "MUTED"
	if !muted {
		statusStr = "UNMUTED"
	}

	notice := &types.Message{
		ID:           "sys-" + generateID(),
		FromUniverse: "earth-prime",
		FromVariant:  "Prime",
		ToUniverse:   "@council",
		Type:         types.MsgRuling,
		Status:       types.StatusDelivered,
		Topic:        "Executive Mute Gavel",
		Content:      fmt.Sprintf("Variant @%s transmitter status set to: %s by Reed Prime.", matched.VariantTag, statusStr),
		CreatedAt:    time.Now().UTC(),
		TTLMinutes:   60,
	}
	_ = h.store.AppendMessage(notice)
	h.broadcastToWatchers(notice)
	return nil
}

// UpdatePowerTier updates the transmitter status of a variant
func (h *Hub) UpdatePowerTier(universeID string, tier types.PowerTier) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	v, ok := h.activeVariants[universeID]
	if !ok {
		return fmt.Errorf("variant not active: %s", universeID)
	}
	v.PowerTier = tier
	v.LastHeartbeat = time.Now().UTC()

	notice := &types.Message{
		ID:           "sys-" + generateID(),
		FromUniverse: "council-chamber",
		FromVariant:  "Sensor Array",
		ToUniverse:   "@council",
		Type:         types.MsgNotice,
		Status:       types.StatusDelivered,
		Topic:        "Transmitter Power Update",
		Content:      fmt.Sprintf("Universe '%s' transmitter power adjusted to: %s.", universeID, tier),
		CreatedAt:    time.Now().UTC(),
	}
	h.broadcastToWatchers(notice)
	return nil
}

// Web Watcher subscription management
func (h *Hub) SubscribeWatcher(id string) chan *types.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan *types.Message, 100)
	h.webWatchers[id] = ch
	return ch
}

func (h *Hub) UnsubscribeWatcher(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.webWatchers[id]; ok {
		close(ch)
		delete(h.webWatchers, id)
	}
}

func (h *Hub) broadcastToWatchers(msg *types.Message) {
	for _, ch := range h.webWatchers {
		select {
		case ch <- msg:
		default:
		}
	}
}

// stalenessSweeper marks expired pending inquiries as STALE silently in the ledger without broadcasting spam
func (h *Hub) stalenessSweeper() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		h.checkStaleness()
	}
}

func (h *Hub) checkStaleness() {
	h.mu.Lock()
	defer h.mu.Unlock()

	_ = h.store.SweepExpiredQuestions(time.Now().UTC())
}

