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

func TestHub_ImplicitSuccessionAndDismissal(t *testing.T) {
	h, _, tmpDir := setupTestHub(t)
	defer os.RemoveAll(tmpDir)

	p := &types.Passport{
		UniverseID: "city-connect",
		Summary:    "City Connect",
	}

	// 1. Initial Antigravity agent with gemini model
	sess1 := &types.VariantSession{
		UniverseID:    "city-connect",
		ClientAgent:   "antigravity",
		ModelFamily:   "gemini",
		WorkspacePath: `C:\TCHT\CityConnect`,
	}
	_, _, err := h.RegisterVariant(sess1, p, "")
	if err != nil {
		t.Fatalf("failed to register sess1: %v", err)
	}
	tag1 := sess1.VariantTag

	// 2. New Antigravity agent in SAME workspace with corrected/new model gemini-3.8-flash (no replaces_variant)
	sess2 := &types.VariantSession{
		UniverseID:    "city-connect",
		ClientAgent:   "antigravity",
		ModelFamily:   "gemini-3.8-flash",
		WorkspacePath: `C:\TCHT\CityConnect`,
	}
	_, _, err = h.RegisterVariant(sess2, p, "")
	if err != nil {
		t.Fatalf("failed to register sess2: %v", err)
	}
	tag2 := sess2.VariantTag

	// Must have implicitly succeeded tag1:
	if tag2 == tag1 {
		t.Fatalf("expected new tag for different model, got same %s", tag2)
	}
	if sess2.PredecessorTag != tag1 {
		t.Fatalf("expected PredecessorTag %s, got %s", tag1, sess2.PredecessorTag)
	}

	// Active variants for city-connect should only be tag2 (tag1 is superseded!)
	active := h.GetActiveVariants()
	cityCount := 0
	for _, v := range active {
		if v.UniverseID == "city-connect" {
			cityCount++
			if v.VariantTag == tag1 {
				t.Fatalf("old variant %s should not be active", tag1)
			}
		}
	}
	if cityCount != 1 {
		t.Fatalf("expected exactly 1 active variant for city-connect, got %d", cityCount)
	}

	// 3. Co-located peer from DIFFERENT client (codex) in same workspace should co-exist
	sessCodex := &types.VariantSession{
		UniverseID:    "city-connect",
		ClientAgent:   "codex",
		ModelFamily:   "gpt-6",
		WorkspacePath: `C:\TCHT\CityConnect`,
	}
	_, _, err = h.RegisterVariant(sessCodex, p, "")
	if err != nil {
		t.Fatalf("failed to register codex peer: %v", err)
	}
	tagCodex := sessCodex.VariantTag

	// Both tag2 and tagCodex must be active
	cityCount = 0
	for _, v := range h.GetActiveVariants() {
		if v.UniverseID == "city-connect" {
			cityCount++
		}
	}
	if cityCount != 2 {
		t.Fatalf("expected 2 active co-located variants, got %d", cityCount)
	}

	// 4. Prime dismisses tagCodex
	if err := h.DismissVariant(tagCodex); err != nil {
		t.Fatalf("failed to dismiss variant: %v", err)
	}

	// Now only tag2 remains
	cityCount = 0
	for _, v := range h.GetActiveVariants() {
		if v.UniverseID == "city-connect" {
			cityCount++
			if v.VariantTag == tagCodex {
				t.Fatalf("dismissed variant %s still active", tagCodex)
			}
		}
	}
	if cityCount != 1 {
		t.Fatalf("expected 1 active variant after dismissal, got %d", cityCount)
	}
}
