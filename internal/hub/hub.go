package hub

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
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
	// Start background routine for TTL / staleness monitoring
	go h.stalenessSweeper()
	return h
}

func generateReedTag(universeID, wsPath string) string {
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

	return fmt.Sprintf("Reed%d-%s", num, shortCode)
}

func generateID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RegisterVariant registers an agent session for a universe
func (h *Hub) RegisterVariant(session *types.VariantSession, passport *types.Passport) (*types.Passport, []*types.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if this universe already exists in store
	existing, found := h.store.GetPassport(session.UniverseID)
	if found {
		existing.VariantEpoch++
		if session.VariantTag == "" {
			session.VariantTag = generateReedTag(session.UniverseID, session.WorkspacePath)
		}
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
		if session.VariantTag == "" {
			session.VariantTag = generateReedTag(session.UniverseID, session.WorkspacePath)
		}
		_ = h.store.UpsertPassport(passport)
	}

	session.ConnectedAt = time.Now().UTC()
	session.LastHeartbeat = session.ConnectedAt
	h.activeVariants[session.UniverseID] = session

	// Ensure message channel exists
	if _, ok := h.listeners[session.UniverseID]; !ok {
		h.listeners[session.UniverseID] = make(chan *types.Message, 50)
	}

	// Fetch pending messages that arrived while offline
	pending := h.store.GetPendingForUniverse(session.UniverseID)

	// Broadcast arrival notice
	notice := &types.Message{
		ID:           "sys-" + generateID(),
		FromUniverse: "council-chamber",
		FromVariant:  "The Herald",
		ToUniverse:   "@council",
		Type:         types.MsgNotice,
		Status:       types.StatusDelivered,
		Topic:        "Variant Arrival",
		Content:      fmt.Sprintf("@%s has arrived to represent '%s' (%s). Power: %s.", session.VariantTag, passport.UniverseName, session.ModelFamily, session.PowerTier),
		CreatedAt:    time.Now().UTC(),
	}
	h.broadcastToWatchers(notice)

	return passport, pending, nil
}

// ResolveTarget finds the matching universe ID for an @mention or universe ID
func (h *Hub) ResolveTarget(target string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	cleaned := strings.TrimPrefix(strings.TrimSpace(target), "@")
	if cleaned == "" || strings.EqualFold(cleaned, "council") {
		return "@council"
	}

	// Match active variant tag (e.g. Reed472-CoR)
	for uid, sess := range h.activeVariants {
		if strings.EqualFold(sess.VariantTag, cleaned) || strings.EqualFold(uid, cleaned) {
			return uid
		}
	}

	// Match stored universe ID
	if _, found := h.store.GetPassport(cleaned); found {
		return cleaned
	}

	return target
}

// PostMessage sends a new message into the Chamber
func (h *Hub) PostMessage(msg *types.Message) (*types.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	msg.ID = "msg-" + generateID()
	msg.CreatedAt = time.Now().UTC()
	msg.Status = types.StatusPending
	if msg.TTLMinutes <= 0 {
		msg.TTLMinutes = 20 // Default 20 minute freshness window
	}

	_ = h.store.AppendMessage(msg)

	// Route to targeted universe if online
	if msg.ToUniverse != "" && msg.ToUniverse != "@council" {
		if targetSession, ok := h.activeVariants[msg.ToUniverse]; ok {
			// Proactively notify agent session in background
			go func(sess *types.VariantSession, m *types.Message) {
				_ = h.dispatcher.Notify(sess, m)
			}(targetSession, msg)
		}

		if ch, ok := h.listeners[msg.ToUniverse]; ok {
			select {
			case ch <- msg:
				msg.Status = types.StatusDelivered
				_ = h.store.UpdateMessageStatus(msg.ID, types.StatusDelivered)
			default:
			}
		}
	} else if msg.ToUniverse == "@council" || msg.ToUniverse == "" {
		// Broadcast to all active listeners except sender
		for uid, sess := range h.activeVariants {
			if uid != msg.FromUniverse {
				go func(s *types.VariantSession, m *types.Message) {
					_ = h.dispatcher.Notify(s, m)
				}(sess, msg)
			}
		}

		for uid, ch := range h.listeners {
			if uid != msg.FromUniverse {
				select {
				case ch <- msg:
				default:
				}
			}
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

	list := make([]*types.VariantSession, 0, len(h.activeVariants))
	for _, v := range h.activeVariants {
		list = append(list, v)
	}
	return list
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

// stalenessSweeper marks expired pending inquiries as STALE
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

	recent := h.store.GetRecentMessages(100)
	now := time.Now().UTC()

	for _, m := range recent {
		if m.Status == types.StatusPending {
			age := now.Sub(m.CreatedAt)
			if age > time.Duration(m.TTLMinutes)*time.Minute {
				m.Status = types.StatusStale
				_ = h.store.UpdateMessageStatus(m.ID, types.StatusStale)

				// Notify sender that query needs re-verification
				staleNotice := &types.Message{
					ID:           "stale-" + generateID(),
					FromUniverse: "council-chamber",
					FromVariant:  "Staleness Monitor",
					ToUniverse:   m.FromUniverse,
					Type:         types.MsgNotice,
					Status:       types.StatusDelivered,
					Topic:        "Stale Inquiry Re-Verification",
					Content:      fmt.Sprintf("Inquiry '%s' to '%s' has exceeded its %d-minute TTL. Please confirm if still needed or withdraw.", m.Topic, m.ToUniverse, m.TTLMinutes),
					InReplyTo:    m.ID,
					CreatedAt:    now,
				}
				if ch, ok := h.listeners[m.FromUniverse]; ok {
					select {
					case ch <- staleNotice:
					default:
					}
				}
				h.broadcastToWatchers(staleNotice)
			}
		}
	}
}

