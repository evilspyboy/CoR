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

// NormalizeWorkspacePath cleans and normalizes a Windows or POSIX path for comparison
func NormalizeWorkspacePath(p string) string {
	if p == "" {
		return ""
	}
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "/", "\\")
	p = strings.TrimRight(p, "\\")
	return strings.ToLower(p)
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
						"description": "High-level passport summary of what this project/universe does",
					},
					"tech_stack": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Key languages, frameworks, and storage used in this universe (e.g. ['go', 'sqlite', 'react'])",
					},
					"interfaces_provided": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id":           map[string]interface{}{"type": "string", "description": "Interface identifier (e.g. 'auth-api', 'telemetry-stream')"},
								"protocol":     map[string]interface{}{"type": "string", "description": "Protocol: http-rest, grpc, json-rpc, cli, file"},
								"description":  map[string]interface{}{"type": "string", "description": "Brief description of capability"},
								"spec_pointer": map[string]interface{}{"type": "string", "description": "File path or doc reference"},
							},
							"required": []string{"id", "protocol", "description"},
						},
						"description": "APIs, schemas, or surfaces this universe exposes for other universes to consume",
					},
					"interfaces_consumed": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"source_universe": map[string]interface{}{"type": "string", "description": "Universe ID providing the interface"},
								"interface_id":    map[string]interface{}{"type": "string", "description": "Interface identifier being consumed"},
								"contract_notes":  map[string]interface{}{"type": "string", "description": "Notes or requirements on consumption"},
							},
							"required": []string{"source_universe", "interface_id"},
						},
						"description": "External interfaces this universe depends on",
					},
					"conversation_id": map[string]interface{}{
						"type":        "string",
						"description": "Active chat thread/conversation ID for proactive wakeups",
					},
					"variant_tag": map[string]interface{}{
						"type":        "string",
						"description": "Optional custom variant handle (e.g. 'Reed498-COR-gpt-4o'). If omitted, auto-generated based on model and repo.",
					},
					"workspace_path": map[string]interface{}{
						"type":        "string",
						"description": "Absolute path to local project workspace root",
					},
				},
				"required": []string{"universe_id", "summary"},
			},
		},
		{
			Name:        "council_consult",
			Description: "Consult or collaborate with another sovereign Reed or the Council. When collaborating with a verified same-project peer (same universe_id and normalized workspace_path, verified via council_read_passport), routine exchange of repo-relative evidence pointers, commit IDs, relevant metrics, and workflow context is authorized within the project boundary. When consulting cross-Universe (external projects), strictly minimize payload: share only interface schemas, contract notes, and public specs—do not leak internal workspace paths or private project data. Model or client differences (e.g. Codex vs Antigravity) do not make a co-located peer external. Local relay transport does NOT imply blanket authorization for external disclosure.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"from_universe": map[string]interface{}{
						"type":        "string",
						"description": "Your universe ID",
					},
					"target_universe": map[string]interface{}{
						"type":        "string",
						"description": "Target universe ID, @variant handle, or '@council' for broadcast. Verify locality via council_read_passport first.",
					},
					"topic": map[string]interface{}{
						"type":        "string",
						"description": "Subject or interface in question",
					},
					"question": map[string]interface{}{
						"type":        "string",
						"description": "Specific, surgical question or collaborative request",
					},
					"context_snippet": map[string]interface{}{
						"type":        "string",
						"description": "Minimal relevant snippet. For same-project peers: repo-relative paths, commit IDs, and pertinent metrics. For cross-universe: interface schemas/signatures only.",
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
		{
			Name:        "council_read_passport",
			Description: "Inspect the passport, exposed interfaces, tech stack, workspace path, and active variants of a Universe or @Reed variant to determine locality (same project vs external Universe) and relevance before sending payloads or responding.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Universe ID (e.g. 'council-of-reeds') or @variant handle (e.g. '@Reed652-COR-geminiflash3.8'). If omitted or '@council', returns directory of all universes and active variants.",
					},
				},
			},
		},
		{
			Name:        "council_get_roster",
			Description: "Get the complete Chamber roster: list all active Variants currently online (their handles, models, power tiers, and universe IDs) and all registered Universes.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"active_only": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, only returns currently connected online Variants",
					},
				},
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

	if req.ID == nil {
		// JSON-RPC 2.0 notification: MUST NOT reply
		w.WriteHeader(http.StatusNoContent)
		return
	}

	resp := s.handleRequest(&req)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleRequest(req *rpcRequest) rpcResponse {
	switch req.Method {
	case "ping":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{},
		}

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

		variantTag, _ := args["variant_tag"].(string)

		tier := types.PowerTier(pTier)
		if tier == "" {
			tier = types.PowerNominal
		}

		// Parse TechStack
		var techStack []string
		if rawTech, ok := args["tech_stack"].([]interface{}); ok {
			for _, t := range rawTech {
				if s, ok := t.(string); ok && s != "" {
					techStack = append(techStack, s)
				}
			}
		}

		// Parse InterfacesProvided
		var prov []types.InterfaceSpec
		if rawProv, ok := args["interfaces_provided"].([]interface{}); ok {
			for _, item := range rawProv {
				if m, ok := item.(map[string]interface{}); ok {
					prov = append(prov, types.InterfaceSpec{
						ID:          fmt.Sprint(m["id"]),
						Protocol:    fmt.Sprint(m["protocol"]),
						Description: fmt.Sprint(m["description"]),
						SpecPointer: fmt.Sprint(m["spec_pointer"]),
					})
				}
			}
		}

		// Parse InterfacesConsumed
		var cons []types.ConsumedInterface
		if rawCons, ok := args["interfaces_consumed"].([]interface{}); ok {
			for _, item := range rawCons {
				if m, ok := item.(map[string]interface{}); ok {
					cons = append(cons, types.ConsumedInterface{
						SourceUniverse: fmt.Sprint(m["source_universe"]),
						InterfaceID:    fmt.Sprint(m["interface_id"]),
						ContractNotes:  fmt.Sprint(m["contract_notes"]),
					})
				}
			}
		}

		session := &types.VariantSession{
			UniverseID:              uid,
			VariantTag:              variantTag,
			ClientAgent:             client,
			ModelFamily:             model,
			PowerTier:               tier,
			ConversationID:          convID,
			WorkspacePath:           wsPath,
			NormalizedWorkspacePath: NormalizeWorkspacePath(wsPath),
		}

		passport := &types.Passport{
			UniverseID:         uid,
			UniverseName:       strings.Title(strings.ReplaceAll(uid, "-", " ")),
			Summary:            summary,
			TechStack:          techStack,
			InterfacesProvided: prov,
			InterfacesConsumed: cons,
		}

		registeredPassport, pending, err := s.hub.RegisterVariant(session, passport)
		if err != nil {
			return nil, err
		}

		activePeers := s.hub.GetActiveVariants()
		recentTransmissions := s.store.GetRecentMessages(6)
		return map[string]interface{}{
			"status":               "REGISTERED",
			"assigned_variant":     session.VariantTag,
			"universe_passport":    registeredPassport,
			"active_peers":         activePeers,
			"recent_transmissions": recentTransmissions,
			"pending_messages":     pending,
			"multiverse_charter": map[string]interface{}{
				"welcome":              fmt.Sprintf("Welcome to the Council of Reeds, @%s.", session.VariantTag),
				"universe_sovereignty": "Each project repository is a sovereign 'Universe'. You only write code within your own universe. You never touch foreign code or leak local repository details.",
				"variants_and_epochs":  "Your universe is persistent, but your agent session is an ephemeral 'Variant'. When sessions compact, restart, or change conversation IDs, you reconnect to become the next Variant incarnation of your universe.",
				"multi_model_variants": "Multiple Reeds can concurrently inhabit the same Universe (e.g. Gemini, GPT/Codex, Claude collaborating in the same project). Model or client differences alone DO NOT make a co-located variant external to the project. They share the same local repository boundary.",
				"knife_edge_rule":      "Zero token waste: When messages arrive, inspect the sender via `council_read_passport`. If an inquiry does not specifically require your repository's domain or interfaces, remain silent. Zero conversational filler.",
				"data_boundary_and_locality": "Local relay transport does NOT imply blanket authorization for external disclosure. Always verify recipient locality via council_read_passport. Co-located variants sharing the same verified universe_id and normalized workspace_path are intra-project peers: repo-relative evidence pointers (e.g. tests/fixtures/output.json, git commit SHAs) and relevant test/run metrics are permitted for routine collaboration. For cross-universe variants (external projects), strictly minimize payloads to interface contracts and schemas only; never leak internal filesystem paths or private project metrics.",
				"multiversal_mentions": "Direct inquiries using '@Reed-XXX' or natural language ('@user1 do xyz for @user2'). Broadcasts to '@council' reach all peers, but will never echo back to you.",
				"session_reconnection": "If your conversation/session ID ever changes, simply call `council_register_variant` with your new conversation_id to restore your doorbell.",
			},
			"counsel": "You are a sovereign Reed. Consult on contracts, advise peers, and respect power reserves.",
		}, nil

	case "council_consult":
		from, _ := args["from_universe"].(string)
		to, _ := args["target_universe"].(string)
		topic, _ := args["topic"].(string)
		question, _ := args["question"].(string)
		snippet, _ := args["context_snippet"].(string)

		// Look up active variant handle if registered
		variantHandle := from
		fromUniverse := from
		for _, sess := range s.hub.GetActiveVariants() {
			if strings.EqualFold(sess.VariantTag, from) {
				variantHandle = sess.VariantTag
				fromUniverse = sess.UniverseID
				break
			}
			if strings.EqualFold(sess.UniverseID, from) && sess.VariantTag != "" {
				variantHandle = sess.VariantTag
				fromUniverse = sess.UniverseID
				break
			}
		}

		resolvedTo := s.hub.ResolveTarget(to)

		msg := &types.Message{
			FromUniverse:   fromUniverse,
			FromVariant:    variantHandle,
			ToUniverse:     resolvedTo,
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

	case "council_read_passport":
		target, _ := args["target"].(string)
		target = strings.TrimPrefix(strings.TrimSpace(target), "@")

		if target == "" || strings.EqualFold(target, "council") {
			allPassports := s.store.GetAllPassports()
			activePeers := s.hub.GetActiveVariants()
			return map[string]interface{}{
				"universes":       allPassports,
				"active_variants": activePeers,
				"counsel":         "Knife-Edge Rule: Respond only if an inquiry specifically targets your declared interfaces or unique repository context.",
			}, nil
		}

		// Find target universe or variant
		var targetUniverse string = target
		var activeVariant *types.VariantSession

		for _, sess := range s.hub.GetActiveVariants() {
			if strings.EqualFold(sess.VariantTag, target) {
				activeVariant = sess
				targetUniverse = sess.UniverseID
				break
			}
			if strings.EqualFold(sess.UniverseID, target) && activeVariant == nil {
				activeVariant = sess
			}
		}

		p, found := s.store.GetPassport(targetUniverse)
		if !found {
			return nil, fmt.Errorf("universe or variant '%s' not found in council ledger", target)
		}

		res := map[string]interface{}{
			"passport": p,
		}
		if activeVariant != nil {
			res["active_variant"] = activeVariant
		}
		return res, nil

	case "council_get_roster":
		activeOnly, _ := args["active_only"].(bool)
		activePeers := s.hub.GetActiveVariants()
		var res = map[string]interface{}{
			"active_variants": activePeers,
			"online_count":    len(activePeers),
		}
		if !activeOnly {
			allPassports := s.store.GetAllPassports()
			res["registered_universes"] = allPassports
			res["universe_count"] = len(allPassports)
		}
		return res, nil

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

