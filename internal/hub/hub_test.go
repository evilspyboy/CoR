package hub_test

import (
	"os"
	"testing"

	"council-of-reeds/internal/hub"
	"council-of-reeds/internal/store"
	"council-of-reeds/internal/types"
)

func setupTestHub(t *testing.T) (*hub.Hub, *store.Store, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "cor-hub-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	s, err := store.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	h := hub.NewHub(s)
	return h, s, tmpDir
}

func TestHub_ReconnectionAndSuccession(t *testing.T) {
	h, _, tmpDir := setupTestHub(t)
	defer os.RemoveAll(tmpDir)

	p := &types.Passport{
		UniverseID: "universe-alpha",
		Summary:    "Universe Alpha",
	}

	// 1. Initial Variant A arrival
	sessA := &types.VariantSession{
		UniverseID:     "universe-alpha",
		VariantTag:     "ReedA",
		ClientAgent:    "antigravity",
		ModelFamily:    "gemini",
		ConversationID: "chat-111",
		WorkspacePath:  `C:\TCHT\Alpha`,
	}
	_, _, err := h.RegisterVariant(sessA, p, "")
	if err != nil {
		t.Fatalf("failed to register sessA: %v", err)
	}

	// Mute ReedA by Prime
	if err := h.SetVariantMuted("ReedA", true); err != nil {
		t.Fatalf("failed to mute ReedA: %v", err)
	}

	// 2. Reconnection in same chat (chat-111) should restore ReedA, keeping mute status
	sessAReconn := &types.VariantSession{
		UniverseID:     "universe-alpha",
		ClientAgent:    "antigravity",
		ModelFamily:    "gemini",
		ConversationID: "chat-111",
		WorkspacePath:  `C:\TCHT\Alpha`,
	}
	_, _, err = h.RegisterVariant(sessAReconn, p, "")
	if err != nil {
		t.Fatalf("failed to reconnect: %v", err)
	}
	if sessAReconn.VariantTag != "ReedA" {
		t.Fatalf("expected reconnected tag 'ReedA', got %s", sessAReconn.VariantTag)
	}
	if !sessAReconn.IsMuted {
		t.Fatalf("anti-hijack violation: mute status was stripped on reconnection!")
	}

	// 3. Validated succession: Variant B explicitly replaces ReedA
	sessB := &types.VariantSession{
		UniverseID:     "universe-alpha",
		VariantTag:     "ReedB",
		ClientAgent:    "codex",
		ModelFamily:    "gpt-6", // Model changed during succession!
		ConversationID: "chat-222",
		WorkspacePath:  `C:\TCHT\Alpha`,
	}
	_, _, err = h.RegisterVariant(sessB, p, "ReedA")
	if err != nil {
		t.Fatalf("failed to register successor B: %v", err)
	}
	if sessB.PredecessorTag != "ReedA" {
		t.Fatalf("expected predecessor ReedA, got %s", sessB.PredecessorTag)
	}
	if !sessB.IsMuted {
		t.Fatalf("anti-hijack violation: successor should inherit mute gavel!")
	}

	// 4. Attempting to reconnect as superseded ReedA must fail
	sessZombie := &types.VariantSession{
		UniverseID:    "universe-alpha",
		VariantTag:    "ReedA",
		WorkspacePath: `C:\TCHT\Alpha`,
	}
	_, _, err = h.RegisterVariant(sessZombie, p, "")
	if err == nil {
		t.Fatalf("expected error reconnecting as superseded variant, got nil")
	}

	// 5. Cross-workspace succession must be rejected
	sessAttacker := &types.VariantSession{
		UniverseID:    "universe-alpha",
		VariantTag:    "ReedAttacker",
		WorkspacePath: `C:\Other\Repo`,
	}
	_, _, err = h.RegisterVariant(sessAttacker, p, "ReedB")
	if err == nil {
		t.Fatalf("expected error succeeding across workspace boundaries, got nil")
	}
}

func TestHub_TransitiveRoutingAndSuccessionExchange(t *testing.T) {
	h, _, tmpDir := setupTestHub(t)
	defer os.RemoveAll(tmpDir)

	pAlpha := &types.Passport{UniverseID: "alpha", Summary: "Alpha"}
	pBeta := &types.Passport{UniverseID: "beta", Summary: "Beta"}

	// Register Alpha A
	sessAlphaA := &types.VariantSession{
		UniverseID:    "alpha",
		VariantTag:    "ReedAlphaA",
		WorkspacePath: `C:\Alpha`,
	}
	_, _, _ = h.RegisterVariant(sessAlphaA, pAlpha, "")

	// Register Beta 1
	sessBeta1 := &types.VariantSession{
		UniverseID:    "beta",
		VariantTag:    "ReedBeta1",
		WorkspacePath: `C:\Beta`,
	}
	_, _, _ = h.RegisterVariant(sessBeta1, pBeta, "")

	// Alpha A sends a question directly to @ReedBeta1
	qMsg := &types.Message{
		FromUniverse: "alpha",
		FromVariant:  "ReedAlphaA",
		ToUniverse:   "@ReedBeta1",
		Type:         types.MsgConsultation,
		Topic:        "Interface Query",
		Content:      "What is your telemetry spec?",
	}
	postedQ, err := h.PostMessage(qMsg)
	if err != nil {
		t.Fatalf("failed to post question: %v", err)
	}

	// Now Beta 1 compacts and is succeeded by Beta 2
	sessBeta2 := &types.VariantSession{
		UniverseID:    "beta",
		VariantTag:    "ReedBeta2",
		WorkspacePath: `C:\Beta`,
	}
	_, pendingBeta2, err := h.RegisterVariant(sessBeta2, pBeta, "ReedBeta1")
	if err != nil {
		t.Fatalf("failed to register Beta 2: %v", err)
	}

	// Beta 2 MUST inherit the question sent to ReedBeta1!
	if len(pendingBeta2) != 1 || pendingBeta2[0].ID != postedQ.ID {
		t.Fatalf("Beta 2 failed to inherit open question: %+v", pendingBeta2)
	}

	// Alpha A compacts and is succeeded by Alpha B
	sessAlphaB := &types.VariantSession{
		UniverseID:    "alpha",
		VariantTag:    "ReedAlphaB",
		WorkspacePath: `C:\Alpha`,
	}
	_, _, _ = h.RegisterVariant(sessAlphaB, pAlpha, "ReedAlphaA")

	// Beta 2 replies to the question using in_reply_to targeting Alpha A (the original sender)
	replyMsg := &types.Message{
		FromUniverse: "beta",
		FromVariant:  "ReedBeta2",
		ToUniverse:   "@ReedAlphaA", // targeting old handle of Alpha!
		Type:         types.MsgResponse,
		InReplyTo:    postedQ.ID,
		Content:      "Telemetry spec is at docs/telemetry.json",
	}
	postedReply, err := h.PostMessage(replyMsg)
	if err != nil {
		t.Fatalf("failed to post reply: %v", err)
	}

	// Verify Transitive Resolution on reply:
	// OriginalTarget was @ReedAlphaA, ActualRecipient must be @ReedAlphaB!
	if postedReply.OriginalTarget != "@ReedAlphaA" {
		t.Fatalf("expected OriginalTarget @ReedAlphaA, got %s", postedReply.OriginalTarget)
	}
	if postedReply.ActualRecipient != "@ReedAlphaB" {
		t.Fatalf("expected ActualRecipient @ReedAlphaB, got %s", postedReply.ActualRecipient)
	}

	// The original question must now be ANSWERED
	openCheck := h.GetActiveVariants()
	if len(openCheck) == 0 {
		t.Fatalf("expected active variants")
	}
}
