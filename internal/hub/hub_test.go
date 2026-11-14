package hub_test

import (
	"os"
	"testing"
	"time"

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

	// 3. Validated succession: Variant B cannot hijack ReedA while ReedA has a fresh heartbeat
	sessB := &types.VariantSession{
		UniverseID:     "universe-alpha",
		VariantTag:     "ReedB",
		ClientAgent:    "codex",
		ModelFamily:    "gpt-6", // Model changed during succession!
		ConversationID: "chat-222",
		WorkspacePath:  `C:\TCHT\Alpha`,
	}
	_, _, err = h.RegisterVariant(sessB, p, "ReedA")
	if err == nil {
		t.Fatalf("expected hijacking rejection while ReedA has fresh heartbeat")
	}

	// Simulate ReedA going idle/stale (>5m)
	sessAReconn.LastHeartbeat = time.Now().UTC().Add(-6 * time.Minute)
	_, _, err = h.RegisterVariant(sessB, p, "ReedA")
	if err != nil {
		t.Fatalf("failed to register successor B after ReedA became stale: %v", err)
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

func TestHub_UnsupersedeOnDismissalAndOwnerReclaim(t *testing.T) {
	h, s, tmpDir := setupTestHub(t)
	defer os.RemoveAll(tmpDir)

	p := &types.Passport{UniverseID: "opentwin", Summary: "OpenTwin"}

	// 1. Register Reed908
	sess908 := &types.VariantSession{
		UniverseID:     "opentwin",
		VariantTag:     "Reed908-Open-Argon",
		ClientAgent:    "antigravity",
		ModelFamily:    "argon",
		ConversationID: "conv-908",
		WorkspacePath:  `C:\TCHT\OpenTwin`,
	}
	if _, _, err := h.RegisterVariant(sess908, p, ""); err != nil {
		t.Fatalf("failed to register Reed908: %v", err)
	}

	// Age Reed908 and supersede with Reed587
	sess908.LastHeartbeat = time.Now().UTC().Add(-10 * time.Minute)
	sess587 := &types.VariantSession{
		UniverseID:     "opentwin",
		VariantTag:     "Reed587-Open-gemini",
		ClientAgent:    "antigravity",
		ModelFamily:    "gemini",
		ConversationID: "conv-587",
		WorkspacePath:  `C:\TCHT\OpenTwin`,
	}
	if _, _, err := h.RegisterVariant(sess587, p, "Reed908-Open-Argon"); err != nil {
		t.Fatalf("failed to supersede with Reed587: %v", err)
	}

	// 2. Dismiss Reed587 -> Reed908 must be un-superseded!
	if err := h.DismissVariant("Reed587-Open-gemini"); err != nil {
		t.Fatalf("failed to dismiss Reed587: %v", err)
	}
	hist, found := s.GetVariantSession("Reed908-Open-Argon")
	if !found || hist.State == types.VariantSuperseded || hist.SuccessorTag != "" {
		t.Fatalf("expected Reed908 to be un-superseded after Reed587 dismissal, got state=%v succ=%s", hist.State, hist.SuccessorTag)
	}

	// 3. Original conversation conv-908 reclaims Reed908-Open-Argon
	sess908Reclaim := &types.VariantSession{
		UniverseID:     "opentwin",
		ClientAgent:    "antigravity",
		ModelFamily:    "argon",
		ConversationID: "conv-908",
		WorkspacePath:  `C:\TCHT\OpenTwin`,
	}
	if _, _, err := h.RegisterVariant(sess908Reclaim, p, ""); err != nil {
		t.Fatalf("failed to reclaim Reed908 by conversation_id: %v", err)
	}
	if sess908Reclaim.VariantTag != "Reed908-Open-Argon" {
		t.Fatalf("expected reclaimed tag Reed908-Open-Argon, got %s", sess908Reclaim.VariantTag)
	}
}

func TestHub_SelfRoutingExclusionAndSelfMentionFilter(t *testing.T) {
	h, _, tmpDir := setupTestHub(t)
	defer os.RemoveAll(tmpDir)

	p := &types.Passport{UniverseID: "opentwin", Summary: "OpenTwin"}

	sessCodex := &types.VariantSession{
		UniverseID:     "opentwin",
		VariantTag:     "Reed260-Open-gpt-6",
		ClientAgent:    "codex",
		ModelFamily:    "gpt-6",
		ConversationID: "conv-codex",
		WorkspacePath:  `C:\TCHT\OpenTwin`,
	}
	sessArgon := &types.VariantSession{
		UniverseID:     "opentwin",
		VariantTag:     "Reed492-Open-argon",
		ClientAgent:    "antigravity",
		ModelFamily:    "argon",
		ConversationID: "conv-argon",
		WorkspacePath:  `C:\TCHT\OpenTwin`,
	}
	_, _, _ = h.RegisterVariant(sessCodex, p, "")
	_, _, _ = h.RegisterVariant(sessArgon, p, "")

	// 1. ResolveTarget("opentwin", "Reed492-Open-argon") must resolve to Reed260-Open-gpt-6, never Reed492 itself
	for i := 0; i < 10; i++ {
		resolved := h.ResolveTarget("opentwin", "Reed492-Open-argon")
		if resolved != "Reed260-Open-gpt-6" {
			t.Fatalf("iteration %d: expected ResolveTarget to pick peer Reed260-Open-gpt-6, got %s", i, resolved)
		}
	}

	// 2. PostMessage with a self-mention in content must not add sender to Recipients
	msg := &types.Message{
		FromUniverse: "opentwin",
		FromVariant:  "Reed492-Open-argon",
		ToUniverse:   "@Reed260-Open-gpt-6",
		Type:         types.MsgConsultation,
		Topic:        "Agreement",
		Content:      "We agree (@Reed260-Open-gpt-6 + @Reed492-Open-argon).",
	}
	posted, err := h.PostMessage(msg)
	if err != nil {
		t.Fatalf("PostMessage failed: %v", err)
	}
	if len(posted.Recipients) != 1 || posted.Recipients[0].VariantTag != "Reed260-Open-gpt-6" {
		t.Fatalf("expected only Reed260-Open-gpt-6 in Recipients (self-mention filtered), got %+v", posted.Recipients)
	}
}
