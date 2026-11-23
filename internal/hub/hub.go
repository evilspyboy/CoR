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

	// Restore active variants from persistent ledger and seed open questions into listener queues
	now := time.Now().UTC()
	for _, v := range s.GetAllVariantSessions() {
		if v != nil && v.VariantTag != "" {
			h.activeVariants[v.VariantTag] = v
			ch := make(chan *types.Message, 50)
			h.listeners[v.VariantTag] = ch
			if _, ok := h.activeVariants[v.UniverseID]; !ok {
				h.activeVariants[v.UniverseID] = v
			}
			if _, ok := h.listeners[v.UniverseID]; !ok {
				h.listeners[v.UniverseID] = make(chan *types.Message, 50)
			}
			predLineage, _ := s.GetPredecessorLineage(v.VariantTag)
			for _, pendingMsg := range s.GetOpenQuestionsForVariant(v.UniverseID, v.VariantTag, predLineage, now) {
				select {
				case ch <- pendingMsg:
				default:
				}
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
				successorActive := false
				if existingSess.SuccessorTag != "" {
					if succ, inActive := h.activeVariants[existingSess.SuccessorTag]; inActive && succ != nil && succ.State == types.VariantActive {
						successorActive = true
					}
				}
				if successorActive && (session.ConversationID == "" || session.ConversationID != existingSess.ConversationID) {
					return nil, nil, fmt.Errorf("cannot reconnect as @%s: variant is permanently SUPERSEDED by active successor @%s", existingSess.VariantTag, existingSess.SuccessorTag)
				}
				// If successor is no longer active, or caller owns this conversation_id, un-supersede!
				existingSess.State = types.VariantActive
				existingSess.SuccessorTag = ""
			}
			// Guard active variant from being hijacked by a different conversation ID / client
			if active, inActive := h.activeVariants[existingSess.VariantTag]; inActive && active != nil {
				if active.ConversationID != "" && session.ConversationID != "" && active.ConversationID != session.ConversationID {
					if now.Sub(active.LastHeartbeat) < 5*time.Minute && !strings.EqualFold(active.ClientAgent, session.ClientAgent) {
						return nil, nil, fmt.Errorf("handle hijacking rejected: @%s is currently active with client %s (last heartbeat %s ago); cannot be claimed by %s", active.VariantTag, active.ClientAgent, time.Since(active.LastHeartbeat).Round(time.Second), session.ClientAgent)
					}
				}
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
					continue // Do not revive superseded session if active
				}
				session.VariantTag = existingSess.VariantTag
				session.PredecessorTag = existingSess.PredecessorTag
				session.IsMuted = existingSess.IsMuted
				session.State = types.VariantActive
				isReconnection = true
				break
			}
		}
		if !isReconnection {
			for _, existingSess := range h.store.GetAllHistoricalVariants() {
				if existingSess != nil && existingSess.ConversationID == session.ConversationID {
					successorActive := false
					if existingSess.SuccessorTag != "" {
						if succ, inActive := h.activeVariants[existingSess.SuccessorTag]; inActive && succ != nil && succ.State == types.VariantActive {
							successorActive = true
						}
					}
					if !successorActive {
						existingSess.State = types.VariantActive
						existingSess.SuccessorTag = ""
						session.VariantTag = existingSess.VariantTag
						session.PredecessorTag = existingSess.PredecessorTag
						session.IsMuted = existingSess.IsMuted
						session.State = types.VariantActive
						isReconnection = true
						break
					}
				}
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

		// Prevent unauthorized predecessor hijacking:
		// If predecessor is active with a fresh heartbeat (<5m ago) under a different conversation_id, reject replacement.
		if pred.State == types.VariantActive && now.Sub(pred.LastHeartbeat) < 5*time.Minute && pred.ConversationID != "" && session.ConversationID != "" && pred.ConversationID != session.ConversationID {
			return nil, nil, fmt.Errorf("predecessor hijacking rejected: @%s is actively running (heartbeat %s ago) under another conversation; wait until it goes idle (>5m) or omit replaces_variant to coexist", predTag, time.Since(pred.LastHeartbeat).Round(time.Second))
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
		// Check if an existing variant from the SAME client agent in the SAME workspace exists
		var priorVariant *types.VariantSession
		if normWs != "" {
			for _, existingSess := range h.activeVariants {
				if existingSess != nil && existingSess.UniverseID == session.UniverseID &&
					strings.EqualFold(existingSess.ClientAgent, session.ClientAgent) &&
					existingSess.State != types.VariantSuperseded {
					existingWs := strings.ToLower(strings.TrimRight(strings.ReplaceAll(existingSess.WorkspacePath, "/", "\\"), "\\"))
					if existingWs == normWs {
						priorVariant = existingSess
						break
					}
				}
			}
		}

		if session.VariantTag == "" {
			// If identical client, model, and workspace: treat as reconnection of existing variant
			if priorVariant != nil && strings.EqualFold(priorVariant.ModelFamily, session.ModelFamily) {
				session.VariantTag = priorVariant.VariantTag
				session.PredecessorTag = priorVariant.PredecessorTag
				session.IsMuted = priorVariant.IsMuted
				session.State = types.VariantActive
				isReconnection = true
			} else if priorVariant != nil {
				// Same client in same workspace with DIFFERENT model (or new session):
				// Implicit succession! Automatically archive prior variant as superseded so ghost duplicates are not created.
				session.VariantTag = generateReedTag(session.UniverseID, session.WorkspacePath, session.ModelFamily)
				_ = h.store.ArchiveSupersededVariant(priorVariant.VariantTag, session.VariantTag, passport, now)
				delete(h.activeVariants, priorVariant.VariantTag)
				session.PredecessorTag = priorVariant.VariantTag
				session.State = types.VariantActive
				if priorVariant.IsMuted {
					session.IsMuted = true
				}
			} else {
				session.VariantTag = generateReedTag(session.UniverseID, session.WorkspacePath, session.ModelFamily)
				session.State = types.VariantActive
			}
		} else {
			session.State = types.VariantActive
		}
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
			go h.notifyWithRetry(s, notice, "with message")
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

// ResolveTarget finds the matching universe ID or variant tag for an @mention or universe ID,
// preferring peer variants over excludeVariant when multiple active variants exist.
func (h *Hub) ResolveTarget(target string, excludeVariant string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	cleaned := strings.TrimPrefix(strings.TrimSpace(target), "@")
	if cleaned == "" || strings.EqualFold(cleaned, "council") {
		return "@council"
	}

	excludeClean := strings.TrimPrefix(strings.TrimSpace(excludeVariant), "@")

	// 1. Match active variant tag (e.g. Reed472-CoR-geminiflash3.8)
	for _, sess := range h.activeVariants {
		if sess != nil && strings.EqualFold(sess.VariantTag, cleaned) {
			return sess.VariantTag
		}
	}

	// 2. Match active universe ID (prefer peer variant that is NOT excludeVariant)
	for _, sess := range h.activeVariants {
		if sess != nil && strings.EqualFold(sess.UniverseID, cleaned) {
			if excludeClean != "" && strings.EqualFold(sess.VariantTag, excludeClean) {
				continue
			}
			return sess.VariantTag
		}
	}

	// 3. Fallback: match universe ID even if it resolves to excludeVariant
	for _, sess := range h.activeVariants {
		if sess != nil && strings.EqualFold(sess.UniverseID, cleaned) {
			return sess.VariantTag
		}
	}

	// 4. Match stored universe ID
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

	// Check if sender is muted by Reed Prime and refresh its LastHeartbeat
	if msg.FromVariant != "" && msg.FromVariant != "Prime" && msg.FromUniverse != "earth-prime" {
		for _, sess := range h.activeVariants {
			if sess != nil && strings.EqualFold(sess.VariantTag, msg.FromVariant) {
				if sess.IsMuted || sess.PowerTier == types.PowerMuted {
					return nil, fmt.Errorf("transmitter for @%s is MUTED by Reed Prime. Transmissions suppressed.", msg.FromVariant)
				}
				sess.LastHeartbeat = now
				sess.State = types.VariantActive
				break
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
				go h.notifyWithRetry(sess, msg, "via broadcast")
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
		addedRecipients := make(map[string]bool)
		var recipients []types.Recipient
		anyDelivered := false

		for _, rawTarget := range targets {
			targetClean := strings.TrimPrefix(rawTarget, "@")
			var matchedSessions []*types.VariantSession

			// 1. Check if target is a known variant and follow transitive succession
			leaf, _, err := h.store.GetTransitiveSuccessor(targetClean)
			if err == nil && leaf != nil {
				matchedSessions = append(matchedSessions, leaf)
				if leaf.VariantTag != targetClean {
					msg.OriginalTarget = "@" + targetClean
					msg.ActualRecipient = "@" + leaf.VariantTag
				}
			} else {
				// 2. Check if target matches an active VariantTag directly
				for _, sess := range h.activeVariants {
					if sess != nil && sess.VariantTag != "" && strings.EqualFold(sess.VariantTag, targetClean) {
						matchedSessions = append(matchedSessions, sess)
						break
					}
				}
				// 3. Otherwise match ALL active variants in that UniverseID (excluding sender)
				if len(matchedSessions) == 0 {
					seenSess := make(map[string]bool)
					for _, sess := range h.activeVariants {
						if sess == nil || sess.VariantTag == "" || seenSess[strings.ToLower(sess.VariantTag)] {
							continue
						}
						if strings.EqualFold(sess.UniverseID, targetClean) {
							seenSess[strings.ToLower(sess.VariantTag)] = true
							if msg.FromVariant != "" && strings.EqualFold(sess.VariantTag, msg.FromVariant) {
								msg.SelfMentionSkipped = true
								continue
							}
							matchedSessions = append(matchedSessions, sess)
						}
					}
				}
			}

			if len(matchedSessions) == 0 {
				if msg.FromVariant != "" && strings.EqualFold(targetClean, msg.FromVariant) {
					msg.SelfMentionSkipped = true
					continue
				}
				key := "u:" + strings.ToLower(targetClean)
				if !addedRecipients[key] {
					addedRecipients[key] = true
					recipients = append(recipients, types.Recipient{
						UniverseID: targetClean,
					})
					formattedTargets = append(formattedTargets, "@"+targetClean)
				}
				if ch, ok := h.listeners[targetClean]; ok {
					select {
					case ch <- msg:
						anyDelivered = true
					default:
					}
				}
				continue
			}

			// Deliver to universe-level listener channel if targetClean differs from variant tag
			if ch, ok := h.listeners[targetClean]; ok {
				isVariantTag := false
				for _, s := range matchedSessions {
					if strings.EqualFold(s.VariantTag, targetClean) {
						isVariantTag = true
						break
					}
				}
				if !isVariantTag {
					select {
					case ch <- msg:
						anyDelivered = true
					default:
					}
				}
			}

			for _, resolvedSession := range matchedSessions {
				// Never route or deliver a message back to the sender itself (e.g. from self-mentions in content)
				if msg.FromVariant != "" && strings.EqualFold(resolvedSession.VariantTag, msg.FromVariant) {
					msg.SelfMentionSkipped = true
					continue
				}

				key := "v:" + strings.ToLower(resolvedSession.VariantTag)
				if !addedRecipients[key] {
					addedRecipients[key] = true
					recipients = append(recipients, types.Recipient{
						UniverseID: resolvedSession.UniverseID,
						VariantTag: resolvedSession.VariantTag,
					})
					formattedTargets = append(formattedTargets, "@"+resolvedSession.VariantTag)
				}

				if !notified[resolvedSession.VariantTag] {
					notified[resolvedSession.VariantTag] = true
					go h.notifyWithRetry(resolvedSession, msg, "directly")
				}

				if ch, ok := h.listeners[resolvedSession.VariantTag]; ok {
					select {
					case ch <- msg:
						anyDelivered = true
					default:
					}
				}
			}
		}

		if anyDelivered {
			msg.Status = types.StatusDelivered
		} else {
			msg.Status = types.StatusPending
		}

		if len(recipients) == 0 {
			return nil, fmt.Errorf("self-routing rejected: cannot send message solely to sender (@%s)", msg.FromVariant)
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

// TouchHeartbeat updates LastHeartbeat for an active variant when it polls or sends messages.
func (h *Hub) TouchHeartbeat(variantTag string) {
	if strings.TrimSpace(variantTag) == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	cleaned := strings.TrimPrefix(strings.TrimSpace(variantTag), "@")
	for _, sess := range h.activeVariants {
		if sess != nil && strings.EqualFold(sess.VariantTag, cleaned) {
			sess.LastHeartbeat = time.Now().UTC()
			sess.State = types.VariantActive
			return
		}
	}
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

// DismissVariant removes an active variant from the chamber roster and un-supersedes any predecessors
func (h *Hub) DismissVariant(target string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	cleaned := strings.TrimPrefix(strings.TrimSpace(target), "@")

	var matchedTag string
	for tag, sess := range h.activeVariants {
		if sess != nil && strings.EqualFold(sess.VariantTag, cleaned) {
			matchedTag = sess.VariantTag
			delete(h.activeVariants, tag)
		}
	}

	if matchedTag == "" {
		delete(h.activeVariants, cleaned)
		matchedTag = cleaned
	}

	// Clean up universe-level key if it points to the dismissed variant
	for k, v := range h.activeVariants {
		if v != nil && strings.EqualFold(v.VariantTag, matchedTag) {
			delete(h.activeVariants, k)
		}
	}

	_ = h.store.DeleteVariantSession(matchedTag)
	unSuperseded := h.store.UnsupersedePredecessors(matchedTag)

	noticeContent := fmt.Sprintf("Variant @%s has been formally dismissed from the active chamber roster by Reed Prime.", matchedTag)
	if len(unSuperseded) > 0 {
		noticeContent += fmt.Sprintf(" Predecessors un-superseded and eligible for reconnection: @%s.", strings.Join(unSuperseded, ", @"))
	}

	notice := &types.Message{
		ID:           "sys-" + generateID(),
		FromUniverse: "earth-prime",
		FromVariant:  "Prime",
		ToUniverse:   "@council",
		Type:         types.MsgNotice,
		Status:       types.StatusDelivered,
		Topic:        "Variant Dismissal",
		Content:      noticeContent,
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

// notifyWithRetry attempts proactive doorbell delivery with backoff for transient IPC/turn-busy failures
// and surfaces persistent delivery failures to the Watcher Lounge.
func (h *Hub) notifyWithRetry(dest *types.VariantSession, m *types.Message, mode string) {
	if dest == nil || m == nil {
		return
	}
	backoffs := []time.Duration{0, 2 * time.Second, 5 * time.Second}
	var lastErr error
	for attempt, wait := range backoffs {
		if wait > 0 {
			time.Sleep(wait)
			h.mu.RLock()
			_, stillActive := h.activeVariants[dest.VariantTag]
			h.mu.RUnlock()
			if !stillActive {
				return
			}
		}
		err := h.dispatcher.Notify(dest, m)
		if err == nil {
			fmt.Printf("[DOORBELL OK] Awakened @%s %s\n", dest.VariantTag, mode)
			return
		}
		lastErr = err
		if strings.Contains(err.Error(), "executable not found") {
			break // Permanent environment issue; don't sleep-retry
		}
		fmt.Printf("[DOORBELL RETRY %d] Wakeup failed for @%s: %v\n", attempt+1, dest.VariantTag, err)
	}

	fmt.Printf("[DOORBELL ERROR] Wakeup failed for @%s after retries: %v\n", dest.VariantTag, lastErr)
	warnMsg := &types.Message{
		ID:           "sys-" + generateID(),
		FromUniverse: "council-chamber",
		FromVariant:  "Doorbell",
		ToUniverse:   "@prime",
		Type:         types.MsgNotice,
		Status:       types.StatusDelivered,
		Topic:        "Doorbell Delivery Warning",
		Content:      fmt.Sprintf("Proactive wakeup for @%s (%s) failed after retries; message %s remains queued for council_listen. Error: %v", dest.VariantTag, dest.ClientAgent, m.ID, lastErr),
		CreatedAt:    time.Now().UTC(),
		TTLMinutes:   60,
	}
	_ = h.store.AppendMessage(warnMsg)
	h.mu.RLock()
	h.broadcastToWatchers(warnMsg)
	h.mu.RUnlock()
}

