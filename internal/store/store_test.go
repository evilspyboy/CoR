package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"council-of-reeds/internal/store"
	"council-of-reeds/internal/types"
)

func setupTestStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "cor-store-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	s, err := store.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return s, tmpDir
}

func TestStore_SuccessionLineageAndCycleProtection(t *testing.T) {
	s, tmpDir := setupTestStore(t)
	defer os.RemoveAll(tmpDir)

	now := time.Now().UTC()
	p := &types.Passport{UniverseID: "test-universe", Summary: "Test Universe Passport"}

	// Create Variant A
	sessA := &types.VariantSession{
		UniverseID:  "test-universe",
		VariantTag:  "ReedA",
		ClientAgent: "antigravity",
		ModelFamily: "gemini",
		State:       types.VariantActive,
	}
	if err := s.UpsertVariantSession(sessA); err != nil {
		t.Fatalf("failed to upsert sessA: %v", err)
	}

	// Archive A -> succeeded by B
	if err := s.ArchiveSupersededVariant("ReedA", "ReedB", p, now); err != nil {
		t.Fatalf("failed to archive A: %v", err)
	}

	// Create Variant B (predecessor ReedA)
	sessB := &types.VariantSession{
		UniverseID:     "test-universe",
		VariantTag:     "ReedB",
		PredecessorTag: "ReedA",
		State:          types.VariantActive,
	}
	if err := s.UpsertVariantSession(sessB); err != nil {
		t.Fatalf("failed to upsert sessB: %v", err)
	}

	// Archive B -> succeeded by C
	if err := s.ArchiveSupersededVariant("ReedB", "ReedC", p, now); err != nil {
		t.Fatalf("failed to archive B: %v", err)
	}

	// Create Variant C (predecessor ReedB)
	sessC := &types.VariantSession{
		UniverseID:     "test-universe",
		VariantTag:     "ReedC",
		PredecessorTag: "ReedB",
		State:          types.VariantActive,
	}
	if err := s.UpsertVariantSession(sessC); err != nil {
		t.Fatalf("failed to upsert sessC: %v", err)
	}

	// Test Predecessor Lineage of C: should be [ReedB, ReedA]
	lineage, err := s.GetPredecessorLineage("ReedC")
	if err != nil {
		t.Fatalf("failed to get lineage for C: %v", err)
	}
	if len(lineage) != 2 || lineage[0] != "ReedB" || lineage[1] != "ReedA" {
		t.Fatalf("unexpected lineage for C: %+v", lineage)
	}

	// Test Transitive Successor: starting from A should resolve to active leaf C
	leaf, hops, err := s.GetTransitiveSuccessor("ReedA")
	if err != nil {
		t.Fatalf("failed to get transitive successor for A: %v", err)
	}
	if leaf.VariantTag != "ReedC" {
		t.Fatalf("expected leaf ReedC, got %s", leaf.VariantTag)
	}
	if len(hops) != 2 || hops[0] != "ReedB" || hops[1] != "ReedC" {
		t.Fatalf("unexpected hops: %+v", hops)
	}

	// Test Historical Passport on A
	vA, found := s.GetVariantSession("ReedA")
	if !found {
		t.Fatalf("ReedA not found in session history")
	}
	if vA.State != types.VariantSuperseded {
		t.Fatalf("expected ReedA state SUPERSEDED, got %s", vA.State)
	}
	if vA.HistoricalPassport == nil || vA.HistoricalPassport.Summary != p.Summary {
		t.Fatalf("expected preserved historical passport on ReedA")
	}
}

func TestStore_QuestionLifecycleAndHandover(t *testing.T) {
	s, tmpDir := setupTestStore(t)
	defer os.RemoveAll(tmpDir)

	now := time.Now().UTC()

	// 1. Create an open question for ReedA
	qMsg := &types.Message{
		ID:             "msg-q1",
		FromUniverse:   "earth-prime",
		FromVariant:    "Prime",
		ToUniverse:     "@ReedA",
		OriginalTarget: "@ReedA",
		Type:           types.MsgConsultation,
		Status:         types.StatusPending,
		QuestionState:  types.QuestionOpen,
		Recipients:     []types.Recipient{{VariantTag: "ReedA"}},
		Topic:          "Cohort Analysis",
		Content:        "What is the cohort path?",
		CreatedAt:      now,
		TTLMinutes:     20,
	}
	if err := s.AppendMessage(qMsg); err != nil {
		t.Fatalf("failed to append question: %v", err)
	}

	// 2. Coexisting peer ReedOther in same universe should NOT receive ReedA's question!
	otherQuestions := s.GetOpenQuestionsForVariant("test-universe", "ReedOther", []string{}, now)
	if len(otherQuestions) != 0 {
		t.Fatalf("isolation violation: ReedOther received ReedA's question: %+v", otherQuestions)
	}

	// 3. Successor ReedB (declaring predecessor ReedA) MUST inherit ReedA's open question!
	sessB := &types.VariantSession{
		UniverseID:     "test-universe",
		VariantTag:     "ReedB",
		PredecessorTag: "ReedA",
		State:          types.VariantActive,
	}
	_ = s.UpsertVariantSession(sessB)

	bQuestions := s.GetOpenQuestionsForVariant("test-universe", "ReedB", []string{"ReedA"}, now)
	if len(bQuestions) != 1 || bQuestions[0].ID != "msg-q1" {
		t.Fatalf("handover failure: ReedB did not inherit msg-q1: %+v", bQuestions)
	}

	// 4. Atomic Closure via in_reply_to by successor ReedB
	replyMsg := &types.Message{
		ID:           "msg-r1",
		FromUniverse: "test-universe",
		FromVariant:  "ReedB",
		ToUniverse:   "earth-prime",
		Type:         types.MsgResponse,
		InReplyTo:    "msg-q1",
		Content:      "The path is tests/fixtures/cohort.json",
		CreatedAt:    now.Add(1 * time.Minute),
	}
	closedQ, closed, err := s.CloseQuestionWithReply(replyMsg, now.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("failed to close question with reply: %v", err)
	}
	if !closed || closedQ.QuestionState != types.QuestionAnswered {
		t.Fatalf("expected question answered, got state %s, closed=%v", closedQ.QuestionState, closed)
	}

	// 5. Subsequent inquiry for ReedB should return 0 open questions
	bQuestionsAfter := s.GetOpenQuestionsForVariant("test-universe", "ReedB", []string{"ReedA"}, now.Add(2*time.Minute))
	if len(bQuestionsAfter) != 0 {
		t.Fatalf("expected 0 open questions after reply, got %d", len(bQuestionsAfter))
	}

	// 6. Late informative reply should be saved without double-closing or error
	lateReply := &types.Message{
		ID:           "msg-r2",
		FromUniverse: "test-universe",
		FromVariant:  "ReedB",
		ToUniverse:   "earth-prime",
		Type:         types.MsgResponse,
		InReplyTo:    "msg-q1",
		Content:      "Additional context note",
		CreatedAt:    now.Add(3 * time.Minute),
	}
	_, lateClosed, err := s.CloseQuestionWithReply(lateReply, now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("late reply returned error: %v", err)
	}
	if lateClosed {
		t.Fatalf("late reply should not double-close question")
	}
}

func TestStore_DynamicExpirationStrictDeadline(t *testing.T) {
	s, tmpDir := setupTestStore(t)
	defer os.RemoveAll(tmpDir)

	now := time.Now().UTC()

	qMsg := &types.Message{
		ID:             "msg-exp",
		FromUniverse:   "earth-prime",
		FromVariant:    "Prime",
		ToUniverse:     "@ReedA",
		OriginalTarget: "@ReedA",
		Type:           types.MsgConsultation,
		Status:         types.StatusPending,
		QuestionState:  types.QuestionOpen,
		Recipients:     []types.Recipient{{VariantTag: "ReedA"}},
		CreatedAt:      now,
		TTLMinutes:     10,
	}
	if err := s.AppendMessage(qMsg); err != nil {
		t.Fatalf("failed to append message: %v", err)
	}

	// strictly within TTL (now + 9m < now + 10m): still OPEN
	q9 := s.GetOpenQuestionsForVariant("test-universe", "ReedA", nil, now.Add(9*time.Minute))
	if len(q9) != 1 {
		t.Fatalf("expected question open at 9m, got %d", len(q9))
	}

	// exactly at or after deadline (now + 10m >= now + 10m): EXPIRED
	q10 := s.GetOpenQuestionsForVariant("test-universe", "ReedA", nil, now.Add(10*time.Minute))
	if len(q10) != 0 {
		t.Fatalf("expected question expired at 10m, got %d", len(q10))
	}

	// Cannot withdraw expired question
	err := s.WithdrawQuestion("msg-exp", "ReedA", now.Add(11*time.Minute))
	if err == nil {
		t.Fatalf("expected error withdrawing expired question, got nil")
	}
}

func TestStore_PersistenceReloadAndLegacyMigration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cor-reload-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s1, err := store.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to init s1: %v", err)
	}

	now := time.Now().UTC()
	mLegacy := &types.Message{
		ID:           "msg-legacy-stale",
		FromUniverse: "earth-prime",
		ToUniverse:   "@ReedOld",
		Type:         types.MsgConsultation,
		Status:       types.StatusPending,
		CreatedAt:    now.Add(-60 * time.Minute), // 60 mins old, TTL 20 -> expired
		TTLMinutes:   20,
	}
	if err := s1.AppendMessage(mLegacy); err != nil {
		t.Fatalf("failed to append legacy msg: %v", err)
	}

	// Reload store from disk
	s2, err := store.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to reload s2: %v", err)
	}

	recent := s2.GetRecentMessages(10)
	if len(recent) != 1 {
		t.Fatalf("expected 1 recent message, got %d", len(recent))
	}
	if recent[0].QuestionState != types.QuestionExpired {
		t.Fatalf("expected legacy message to be migrated to EXPIRED, got %s", recent[0].QuestionState)
	}
	if len(recent[0].Recipients) == 0 || recent[0].Recipients[0].VariantTag != "ReedOld" {
		t.Fatalf("expected recipient migrated to structured format: %+v", recent[0].Recipients)
	}
}

func TestStore_CrashRecoveryFromBackup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cor-backup-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := store.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	p := &types.Passport{UniverseID: "universe-backup-test", Summary: "Durable Passport"}
	if err := s.UpsertPassport(p); err != nil {
		t.Fatalf("failed to upsert passport: %v", err)
	}

	// Simulate crash where primary file is truncated/missing but .bak exists
	primaryPath := filepath.Join(tmpDir, "council_ledger.json")
	bakPath := filepath.Join(tmpDir, "council_ledger.json.bak")
	data, err := os.ReadFile(primaryPath)
	if err != nil {
		t.Fatalf("failed to read primary ledger: %v", err)
	}
	if err := os.WriteFile(bakPath, data, 0644); err != nil {
		t.Fatalf("failed to write bak: %v", err)
	}
	_ = os.Remove(primaryPath) // simulate missing primary

	// Reopen store; should recover from .bak automatically
	sRecovered, err := store.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to recover store from .bak: %v", err)
	}

	recoveredP, found := sRecovered.GetPassport("universe-backup-test")
	if !found || recoveredP.Summary != "Durable Passport" {
		t.Fatalf("failed to recover passport from backup: %+v", recoveredP)
	}
}
