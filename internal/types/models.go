package types

import "time"

// PowerTier indicates the agent's compute/token capacity
type PowerTier string

const (
	PowerInfinite PowerTier = "INFINITE" // Local LLM / Ollama
	PowerNominal  PowerTier = "NOMINAL"  // High quota / Cloud standard
	PowerWeak     PowerTier = "WEAK"     // Approaching quota limit (<20%)
	PowerCritical PowerTier = "CRITICAL" // Emergency reserves (<5%)
	PowerOffline  PowerTier = "OFFLINE"  // Disconnected / Quota depleted
)

// InterfaceSpec describes an exposed or consumed interface
type InterfaceSpec struct {
	ID          string `json:"id"`
	Protocol    string `json:"protocol"` // e.g. "http-rest", "websocket-protobuf", "json-file"
	Description string `json:"description"`
	SpecPointer string `json:"spec_pointer,omitempty"` // file path or anchor
}

// ConsumedInterface describes a dependency on another universe
type ConsumedInterface struct {
	SourceUniverse string `json:"source_universe"`
	InterfaceID    string `json:"interface_id"`
	ContractNotes  string `json:"contract_notes,omitempty"`
}

// Passport represents the persistent identity and surface of a Universe
type Passport struct {
	UniverseID          string              `json:"universe_id"`
	UniverseName        string              `json:"universe_name"`
	Summary             string              `json:"summary"`
	TechStack           []string            `json:"tech_stack"`
	VariantEpoch        int                 `json:"variant_epoch"`
	InterfacesProvided  []InterfaceSpec     `json:"interfaces_provided"`
	InterfacesConsumed  []ConsumedInterface `json:"interfaces_consumed"`
	LastActive          time.Time           `json:"last_active"`
}

// VariantSession is the ephemeral incarnation of a Reed currently online
type VariantSession struct {
	UniverseID      string    `json:"universe_id"`
	VariantTag      string    `json:"variant_tag"` // e.g. "Variant 1.2"
	ClientAgent     string    `json:"client_agent"` // e.g. "antigravity", "cursor", "claude-code"
	ModelFamily     string    `json:"model_family"` // e.g. "gemini-1.5-pro", "claude-3-5-sonnet"
	PowerTier       PowerTier `json:"power_tier"`
	ConversationID  string    `json:"conversation_id,omitempty"`
	WorkspacePath   string    `json:"workspace_path,omitempty"`
	ConnectedAt     time.Time `json:"connected_at"`
	LastHeartbeat   time.Time `json:"last_heartbeat"`
}

// MessageType classifies chamber transmissions
type MessageType string

const (
	MsgConsultation MessageType = "CONSULTATION" // Targeted @target query
	MsgProposal     MessageType = "PROPOSAL"     // Proposed interface/schema change
	MsgResponse     MessageType = "RESPONSE"     // Direct answer to a consultation
	MsgEscalation   MessageType = "ESCALATION"   // Dispatched to Reed Prime
	MsgRuling       MessageType = "RULING"       // Reed Prime decree
	MsgNotice       MessageType = "NOTICE"       // System / Stale / Power notice
)

// MessageStatus tracks lifecycle and freshness
type MessageStatus string

const (
	StatusPending   MessageStatus = "PENDING"
	StatusDelivered MessageStatus = "DELIVERED"
	StatusAnswered  MessageStatus = "ANSWERED"
	StatusStale     MessageStatus = "STALE"     // Exceeded TTL, needs confirmation
	StatusWithdrawn MessageStatus = "WITHDRAWN" // Originator resolved locally
)

// Message is an individual transmission in the Chamber
type Message struct {
	ID             string        `json:"id"`
	FromUniverse   string        `json:"from_universe"`
	FromVariant    string        `json:"from_variant"`
	ToUniverse     string        `json:"to_universe"` // Empty or "@council" for broadcast
	Type           MessageType   `json:"type"`
	Status         MessageStatus `json:"status"`
	Topic          string        `json:"topic"`
	Content        string        `json:"content"`
	ContextSnippet string        `json:"context_snippet,omitempty"`
	InReplyTo      string        `json:"in_reply_to,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	TTLMinutes     int           `json:"ttl_minutes"`
}

