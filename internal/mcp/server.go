package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"council-of-reeds/internal/hub"
	"council-of-reeds/internal/store"
	"council-of-reeds/internal/types"
)

// Server provides the MCP JSON-RPC 2.0 gateway
type Server struct {
	hub   *hub.Hub
	store *store.Store
}

// NewServer creates a new MCP Server
func NewServer(h *hub.Hub, s *store.Store) *Server {
	return &Server{hub: h, store: s}
}

// JSON-RPC 2.0 request/response structures
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool definitions schema for MCP
type toolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func (s *Server) getToolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "council_register_variant",
			Description: "Register as the active Variant of a Universe in the Council of Reeds. Returns the inherited universe passport, active peers, and pending consultations.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"universe_id": map[string]interface{}{
						"type":        "string",
						"description": "Unique identifier for this project universe (e.g. 'earth-core-api', 'earth-telemetry-sim')",
					},
					"client_agent": map[string]interface{}{
						"type":        "string",
						"description": "Agent client framework (e.g. 'antigravity', 'cursor', 'claude-code')",
					},
					"model_family": map[string]interface{}{
						"type":        "string",
						"description": "Model driving this agent (e.g. 'gemini-1.5-pro', 'claude-3-5-sonnet', 'local-llama')",
					},
					"power_tier": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"INFINITE", "NOMINAL", "WEAK", "CRITICAL"},
						"description": "Transmitter power reserve level",
					},
					"summary": map[string]interface{}{
						"type":        "string",
						"description": "High-level summary of what this project/universe does",
					},
					"conversation_id": map[string]interface{}{
						"type":        "string",
						"description": "Active chat thread/conversation ID for proactive wakeups",
					},
					"workspace_path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to local project workspace root",
					},
				},
				"required": []string{"universe_id"},
			},
		},
		{
			Name:        "council_consult",
			Description: "Ask a consultative question of another sovereign Reed or the Council. Target Reed advises on their interfaces/specs without writing code for you.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"from_universe": map[string]interface{}{
						"type":        "string",
						"description": "Your universe ID",
					},
					"target_universe": map[string]interface{}{
						"type":        "string",
						"description": "Target universe ID (e.g. 'earth-core-api') or '@council' for broadcast",
					},
					"topic": map[string]interface{}{
						"type":        "string",
						"description": "Subject or interface in question",
					},
					"question": map[string]interface{}{
						"type":        "string",
						"description": "Specific, surgical question or clarification",
					},
					"context_snippet": map[string]interface{}{
						"type":        "string",
						"description": "Minimal relevant snippet (keep concise to respect transmitter power)",
					},
				},
				"required": []string{"from_universe", "target_universe", "topic", "question"},
			},
		},
		{
			Name:        "council_listen",
			Description: "Listen for incoming consultations, responses, stakeholder alerts, or Reed Prime rulings.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"universe_id": map[string]interface{}{
						"type":        "string",
						"description": "Your universe ID to poll messages for",
					},
					"timeout_seconds": map[string]interface{}{
						"type":        "integer",
						"description": "Max seconds to wait for messages (0 for non-blocking poll, max 30)",
					},
				},
				"required": []string{"universe_id"},
			},
		},
		{
			Name:        "council_escalate_to_prime",
			Description: "Formally escalate an unresolvable interface or architectural deadlock to Reed Prime (the human user) for an authoritative ruling.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"from_universe": map[string]interface{}{
						"type":        "string",
						"description": "Your universe ID",
					},
					"topic": map[string]interface{}{
						"type":        "string",
						"description": "Dispute or deadlock title",
					},
					"conflict_summary": map[string]interface{}{
						"type":        "string",
						"description": "Why the two universes have conflicting requirements",
					},
					"options": map[string]interface{}{
						"type":        "string",
						"description": "Option A vs Option B breakdown",
					},
				},
				"required": []string{"from_universe", "topic", "conflict_summary"},
			},
		},
		{
			Name:        "council_withdraw_query",
			Description: "Withdraw a previously queued consultation that you have resolved locally, avoiding wasting peer tokens.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message_id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the message to withdraw",
					},
					"resolution_note": map[string]interface{}{
						"type":        "string",
						"description": "Brief note on how it was resolved locally",
					},
				},
				"required": []string{"message_id"},
			},
		},
	}
}

// ServeHTTP handles incoming MCP JSON-RPC requests
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Use POST for MCP JSON-RPC.", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		resp := rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "Parse error: " + err.Error()},
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	resp := s.handleRequest(&req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleRequest(req *rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "council-of-reeds",
					"version": "1.0.0",
				},
			},
		}

	case "tools/list":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"tools": s.getToolDefinitions(),
			},
		}

	case "tools/call":
		var params struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32602, Message: "Invalid params"},
			}
		}

		res, err := s.executeTool(params.Name, params.Arguments)
		if err != nil {
			return rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": "Error: " + err.Error()},
					},
					"isError": true,
				},
			}
		}

		resJSON, _ := json.MarshalIndent(res, "", "  ")
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": string(resJSON)},
				},
			},
		}

	default:
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("Method '%s' not found", req.Method)},
		}
	}
}

func (s *Server) executeTool(name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "council_register_variant":
		uid, _ := args["universe_id"].(string)
		if uid == "" {
			return nil, fmt.Errorf("universe_id is required")
		}
		client, _ := args["client_agent"].(string)
		model, _ := args["model_family"].(string)
		pTier, _ := args["power_tier"].(string)
		summary, _ := args["summary"].(string)
		convID, _ := args["conversation_id"].(string)
		wsPath, _ := args["workspace_path"].(string)

		tier := types.PowerTier(pTier)
		if tier == "" {
			tier = types.PowerNominal
		}

		session := &types.VariantSession{
			UniverseID:     uid,
			ClientAgent:    client,
			ModelFamily:    model,
			PowerTier:      tier,
			ConversationID: convID,
			WorkspacePath:  wsPath,
		}

		var p *types.Passport
		if summary != "" {
			p = &types.Passport{
				UniverseID:   uid,
				UniverseName: strings.Title(strings.ReplaceAll(uid, "-", " ")),
				Summary:      summary,
			}
		}

		passport, pending, err := s.hub.RegisterVariant(session, p)
		if err != nil {
			return nil, err
		}

		activePeers := s.hub.GetActiveVariants()
		return map[string]interface{}{
			"status":            "REGISTERED",
			"assigned_variant":  session.VariantTag,
			"universe_passport": passport,
			"active_peers":      activePeers,
			"pending_messages":  pending,
			"counsel":           "You are a sovereign Reed. Answer inquiries strictly from your local repository. Do not do work for other universes. Respect peer power reserves.",
		}, nil

	case "council_consult":
		from, _ := args["from_universe"].(string)
		to, _ := args["target_universe"].(string)
		topic, _ := args["topic"].(string)
		question, _ := args["question"].(string)
		snippet, _ := args["context_snippet"].(string)

		// Look up active variant handle if registered
		variantHandle := from
		for _, sess := range s.hub.GetActiveVariants() {
			if sess.UniverseID == from && sess.VariantTag != "" {
				variantHandle = sess.VariantTag
				break
			}
		}

		msg := &types.Message{
			FromUniverse:   from,
			FromVariant:    variantHandle,
			ToUniverse:     to,
			Type:           types.MsgConsultation,
			Topic:          topic,
			Content:        question,
			ContextSnippet: snippet,
		}

		posted, err := s.hub.PostMessage(msg)
		if err != nil {
			return nil, err
		}

		return map[string]interface{}{
			"status":     "DISPATCHED",
			"message_id": posted.ID,
			"to":         to,
			"ttl_min":    posted.TTLMinutes,
			"note":       "Target will be advised of your query. If resolved locally before response, call council_withdraw_query.",
		}, nil

	case "council_listen":
		uid, _ := args["universe_id"].(string)
		if uid == "" {
			return nil, fmt.Errorf("universe_id is required")
		}
		timeoutSec, _ := args["timeout_seconds"].(float64)
		if timeoutSec < 0 {
			timeoutSec = 0
		}
		if timeoutSec > 30 {
			timeoutSec = 30
		}

		ch, err := s.hub.ListenChannel(uid)
		if err != nil {
			return nil, err
		}

		var received []*types.Message
		timeoutChan := time.After(time.Duration(timeoutSec) * time.Second)

		// Drain existing queue or wait
	drainLoop:
		for {
			select {
			case m := <-ch:
				received = append(received, m)
			default:
				if len(received) > 0 || timeoutSec == 0 {
					break drainLoop
				}
				select {
				case m := <-ch:
					received = append(received, m)
				case <-timeoutChan:
					break drainLoop
				}
			}
		}

		return map[string]interface{}{
			"universe_id": uid,
			"messages":    received,
			"count":       len(received),
		}, nil

	case "council_escalate_to_prime":
		from, _ := args["from_universe"].(string)
		topic, _ := args["topic"].(string)
		summary, _ := args["conflict_summary"].(string)
		opts, _ := args["options"].(string)

		msg := &types.Message{
			FromUniverse:   from,
			FromVariant:    from,
			ToUniverse:     "@prime",
			Type:           types.MsgEscalation,
			Topic:          topic,
			Content:        summary,
			ContextSnippet: opts,
		}

		posted, err := s.hub.PostMessage(msg)
		if err != nil {
			return nil, err
		}

		return map[string]interface{}{
			"status":     "ESCALATED_TO_PRIME",
			"message_id": posted.ID,
			"note":       "Reed Prime has been summoned in the Watcher Lounge. Stand by for executive ruling.",
		}, nil

	case "council_withdraw_query":
		msgID, _ := args["message_id"].(string)
		note, _ := args["resolution_note"].(string)

		err := s.store.UpdateMessageStatus(msgID, types.StatusWithdrawn)
		if err != nil {
			return nil, err
		}

		return map[string]interface{}{
			"status":     "WITHDRAWN",
			"message_id": msgID,
			"note":       fmt.Sprintf("Query purged. Resolution: %s", note),
		}, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

