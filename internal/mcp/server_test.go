package mcp_test

import (
	"os"
	"testing"

	"council-of-reeds/internal/hub"
	"council-of-reeds/internal/mcp"
	"council-of-reeds/internal/store"
	"council-of-reeds/internal/types"
)

func setupTestMCP(t *testing.T) (*mcp.Server, *hub.Hub, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "cor-mcp-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	s, err := store.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	h := hub.NewHub(s)
	srv := mcp.NewServer(h, s)
	return srv, h, tmpDir
}

func TestMCP_DismissVariantAuthorization(t *testing.T) {
	srv, h, tmpDir := setupTestMCP(t)
	defer os.RemoveAll(tmpDir)

	pOpen := &types.Passport{UniverseID: "opentwin", Summary: "OpenTwin"}
	pCity := &types.Passport{UniverseID: "city-connect", Summary: "CityConnect"}

	_, _, _ = h.RegisterVariant(&types.VariantSession{
		UniverseID:    "opentwin",
		VariantTag:    "ReedOpen1",
		ClientAgent:   "antigravity",
		WorkspacePath: `C:\TCHT\OpenTwin`,
	}, pOpen, "")
	_, _, _ = h.RegisterVariant(&types.VariantSession{
		UniverseID:    "opentwin",
		VariantTag:    "ReedOpen2",
		ClientAgent:   "codex",
		WorkspacePath: `C:\TCHT\OpenTwin`,
	}, pOpen, "")
	_, _, _ = h.RegisterVariant(&types.VariantSession{
		UniverseID:    "city-connect",
		VariantTag:    "ReedCity1",
		ClientAgent:   "antigravity",
		WorkspacePath: `C:\TCHT\CityConnect`,
	}, pCity, "")

	// 1. Missing caller_variant must be rejected
	if _, err := srv.ExecuteToolForTest("council_dismiss_variant", map[string]interface{}{
		"variant_tag": "ReedOpen1",
	}); err == nil {
		t.Fatalf("expected error when caller_variant is omitted")
	}

	// 2. Cross-universe dismissal must be rejected
	if _, err := srv.ExecuteToolForTest("council_dismiss_variant", map[string]interface{}{
		"variant_tag":    "ReedOpen1",
		"caller_variant": "ReedCity1",
	}); err == nil {
		t.Fatalf("expected error when foreign universe tries to dismiss ReedOpen1")
	}

	// 3. Co-located peer in same workspace may dismiss
	if _, err := srv.ExecuteToolForTest("council_dismiss_variant", map[string]interface{}{
		"variant_tag":    "ReedOpen2",
		"caller_variant": "ReedOpen1",
	}); err != nil {
		t.Fatalf("expected co-located dismissal to succeed, got: %v", err)
	}

	// 4. Self-dismissal may succeed
	if _, err := srv.ExecuteToolForTest("council_dismiss_variant", map[string]interface{}{
		"variant_tag":    "ReedOpen1",
		"caller_variant": "ReedOpen1",
	}); err != nil {
		t.Fatalf("expected self-dismissal to succeed, got: %v", err)
	}
}

func TestMCP_ConsultTargetVariantAndSelfRoutingRejection(t *testing.T) {
	srv, h, tmpDir := setupTestMCP(t)
	defer os.RemoveAll(tmpDir)

	pOpen := &types.Passport{UniverseID: "opentwin", Summary: "OpenTwin"}
	pCity := &types.Passport{UniverseID: "city-connect", Summary: "CityConnect"}

	_, _, _ = h.RegisterVariant(&types.VariantSession{
		UniverseID:    "opentwin",
		VariantTag:    "ReedOpen1",
		ClientAgent:   "antigravity",
		WorkspacePath: `C:\TCHT\OpenTwin`,
	}, pOpen, "")
	_, _, _ = h.RegisterVariant(&types.VariantSession{
		UniverseID:    "opentwin",
		VariantTag:    "ReedOpen2",
		ClientAgent:   "codex",
		WorkspacePath: `C:\TCHT\OpenTwin`,
	}, pOpen, "")
	_, _, _ = h.RegisterVariant(&types.VariantSession{
		UniverseID:    "city-connect",
		VariantTag:    "ReedCity1",
		ClientAgent:   "antigravity",
		WorkspacePath: `C:\TCHT\CityConnect`,
	}, pCity, "")

	// 1. Single variant in city-connect targeting city-connect -> self-routing rejected
	if _, err := srv.ExecuteToolForTest("council_consult", map[string]interface{}{
		"from_universe":   "city-connect",
		"from_variant":    "ReedCity1",
		"target_universe": "city-connect",
		"topic":           "Self test",
		"question":        "Hello me?",
	}); err == nil {
		t.Fatalf("expected self-routing rejection when ReedCity1 targets city-connect with no peer")
	}

	// 2. target_variant routes directly to ReedOpen2
	res, err := srv.ExecuteToolForTest("council_consult", map[string]interface{}{
		"from_universe":   "opentwin",
		"from_variant":    "ReedOpen1",
		"target_universe": "opentwin",
		"target_variant":  "ReedOpen2",
		"topic":           "Peer test",
		"question":        "Hello peer",
	})
	if err != nil {
		t.Fatalf("expected consultation via target_variant to succeed, got: %v", err)
	}
	resMap := res.(map[string]interface{})
	if resMap["to"] != "ReedOpen2" {
		t.Fatalf("expected 'to' to be ReedOpen2, got %v", resMap["to"])
	}

	// 3. Message to peer that also mentions self returns a warning
	resWarn, err := srv.ExecuteToolForTest("council_consult", map[string]interface{}{
		"from_universe":   "opentwin",
		"from_variant":    "ReedOpen1",
		"target_universe": "opentwin",
		"target_variant":  "ReedOpen2",
		"topic":           "Self mention warning",
		"question":        "Agreed (@ReedOpen1 + @ReedOpen2)",
	})
	if err != nil {
		t.Fatalf("expected consultation with self-mention to succeed with warning, got: %v", err)
	}
	resWarnMap := resWarn.(map[string]interface{})
	if _, ok := resWarnMap["warning"]; !ok {
		t.Fatalf("expected 'warning' field when sender also mentioned itself, got %+v", resWarnMap)
	}
}

func TestMCP_ListenIncludeOpenRecovery(t *testing.T) {
	srv, h, tmpDir := setupTestMCP(t)
	defer os.RemoveAll(tmpDir)

	pDP := &types.Passport{UniverseID: "desk-pal", Summary: "Desk Pal"}
	pBmcb := &types.Passport{UniverseID: "bmcbf", Summary: "BMCBF"}
	_, _, _ = h.RegisterVariant(&types.VariantSession{
		UniverseID:    "desk-pal",
		VariantTag:    "ReedDP",
		ClientAgent:   "antigravity",
		WorkspacePath: `C:\TCHT\desk-pal`,
	}, pDP, "")
	_, _, _ = h.RegisterVariant(&types.VariantSession{
		UniverseID:    "bmcbf",
		VariantTag:    "ReedBmcb",
		ClientAgent:   "antigravity",
		WorkspacePath: `C:\TCHT\bmcbf`,
	}, pBmcb, "")

	posted, err := h.PostMessage(&types.Message{
		FromUniverse: "desk-pal",
		FromVariant:  "ReedDP",
		ToUniverse:   "@ReedBmcb",
		Type:         types.MsgConsultation,
		Topic:        "Missed wakeup test",
		Content:      "Can you see this?",
	})
	if err != nil {
		t.Fatalf("PostMessage failed: %v", err)
	}

	// First drain the in-memory channel
	_, _ = srv.ExecuteToolForTest("council_listen", map[string]interface{}{
		"universe_id": "bmcbf",
		"variant_tag": "ReedBmcb",
	})

	// Second call without include_open should be empty
	resEmpty, err := srv.ExecuteToolForTest("council_listen", map[string]interface{}{
		"universe_id": "bmcbf",
		"variant_tag": "ReedBmcb",
	})
	if err != nil {
		t.Fatalf("council_listen failed: %v", err)
	}
	if resEmpty.(map[string]interface{})["count"].(int) != 0 {
		t.Fatalf("expected 0 messages after draining in-memory queue")
	}

	// Third call with include_open=true recovers the unanswered OPEN question
	resOpen, err := srv.ExecuteToolForTest("council_listen", map[string]interface{}{
		"universe_id":  "bmcbf",
		"variant_tag":  "ReedBmcb",
		"include_open": true,
	})
	if err != nil {
		t.Fatalf("council_listen(include_open=true) failed: %v", err)
	}
	openMap := resOpen.(map[string]interface{})
	if openMap["count"].(int) != 1 {
		t.Fatalf("expected 1 recovered OPEN question, got %d", openMap["count"].(int))
	}
	msgs := openMap["messages"].([]*types.Message)
	if msgs[0].ID != posted.ID {
		t.Fatalf("expected recovered message %s, got %s", posted.ID, msgs[0].ID)
	}
}
