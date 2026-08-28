package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Card shapes over MCP (CW-33).
//
// The matrix here is the contract from CW-31/CW-32: what gets seeded for each
// type, what is deliberately NOT seeded, that caller-supplied sections survive,
// and that an update merges rather than replaces.

// shapeEnvelope is the create_ticket result: the ticket's own fields, flattened,
// plus the three envelope keys.
type shapeEnvelope struct {
	ID         string          `json:"id"`
	Key        string          `json:"key"`
	TicketType string          `json:"ticketType"`
	CardData   json.RawMessage `json:"cardData"`
	Shaped     []string        `json:"shaped"`
	Omitted    []string        `json:"omitted"`
	Hint       string          `json:"hint"`
}

func (e shapeEnvelope) card(t *testing.T) map[string]any {
	t.Helper()
	m := map[string]any{}
	if len(e.CardData) == 0 {
		return m
	}
	if err := json.Unmarshal(e.CardData, &m); err != nil {
		t.Fatalf("cardData not an object: %v (%s)", err, e.CardData)
	}
	return m
}

func hasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// shapesBoard spins up an app plus a board to hang tickets off.
func shapesBoard(t *testing.T, org, tenant, user string) (http.Handler, BoardView) {
	t.Helper()
	a := &app{store: testDB(t), logBuf: NewLogBuffer()}
	h := withCORS(a.routes(withTestClaims))
	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Shapes", "keyPrefix": "SHP"}, &board)
	return h, board
}

// A vague ask — title only — still produces a usable story card. This is the
// scenario the whole feature exists for: the user said one sentence and the
// agent should not have to interrogate them before writing anything down.
func TestCreateTicketSeedsStoryFromTitleOnly(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, org, tenant, user)

	var env shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId": board.ID,
		"title":   "Header needs login and register links",
	}, &env)

	if env.TicketType != TicketTypeStory {
		t.Fatalf("ticketType = %q, want story", env.TicketType)
	}
	card := env.card(t)
	if _, ok := card[SectionStory]; !ok {
		t.Fatalf("story block missing: %v", card)
	}
	if _, ok := card[SectionAcceptance]; !ok {
		t.Fatalf("acceptance missing: %v", card)
	}
	// Seeded blocks are empty, so they are not reported as shaped content.
	if !hasString(env.Omitted, SectionScenarios) || !hasString(env.Omitted, SectionUAT) {
		t.Fatalf("omitted = %v, want scenarios and uat", env.Omitted)
	}
	if _, ok := card[SectionUAT]; ok {
		t.Fatalf("uat should not be seeded onto a bare story: %v", card)
	}
	if env.Hint == "" {
		t.Fatal("hint should tell the agent where shapes live")
	}
	if card[SectionSource] != SourceAgent {
		t.Fatalf("source = %v, want agent when the shape was inferred", card[SectionSource])
	}
}

// A defect gets a defect block and explicitly NOT a story block — the whole
// point of separating type from section.
func TestCreateTicketDefectDoesNotSeedStory(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, org, tenant, user)

	var env shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId":    board.ID,
		"title":      "Upload returns 500 on large png",
		"ticketType": "defect",
	}, &env)

	if env.TicketType != TicketTypeDefect {
		t.Fatalf("ticketType = %q, want defect", env.TicketType)
	}
	card := env.card(t)
	if _, ok := card[SectionDefect]; !ok {
		t.Fatalf("defect block missing: %v", card)
	}
	if _, ok := card[SectionStory]; ok {
		t.Fatalf("story must not be auto-seeded onto a defect: %v", card)
	}
}

// A spike's question is the one slot that can be filled honestly from the title.
func TestCreateTicketSpikeFillsQuestionAndSkipsUAT(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, org, tenant, user)

	title := "Can we drop nginx in front of byz-files?"
	var env shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId":    board.ID,
		"title":      title,
		"ticketType": "spike",
	}, &env)

	card := env.card(t)
	spike, ok := card[SectionSpike].(map[string]any)
	if !ok {
		t.Fatalf("spike block missing: %v", card)
	}
	if spike["question"] != title {
		t.Fatalf("spike.question = %v, want the title", spike["question"])
	}
	if _, ok := card[SectionUAT]; ok {
		t.Fatalf("a spike must not get a uat: %v", card)
	}
	if _, ok := card[SectionStory]; ok {
		t.Fatalf("a spike must not get a story: %v", card)
	}
	if len(env.Omitted) != 0 {
		t.Fatalf("omitted = %v, want nothing offered on a spike", env.Omitted)
	}
}

// A chore is work that just needs doing. No story, no UAT ceremony.
func TestCreateTicketChoreStaysBare(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, org, tenant, user)

	var env shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId":    board.ID,
		"title":      "Rotate the staging DB password",
		"ticketType": "chore",
		"seedShapes": true,
	}, &env)

	card := env.card(t)
	if _, ok := card[SectionStory]; ok {
		t.Fatalf("chore must not get a story: %v", card)
	}
	if _, ok := card[SectionUAT]; ok {
		t.Fatalf("chore must not get a uat: %v", card)
	}
	if _, ok := card[SectionChore]; !ok {
		t.Fatalf("chore block missing: %v", card)
	}
}

// When the user DID name the shapes, the agent can send them on the same create
// call — no stub-then-patch.
func TestCreateTicketKeepsCallerScenariosAndUAT(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, org, tenant, user)

	var env shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId": board.ID,
		"title":   "Header login and register links",
		"cardData": map[string]any{
			"uat": []string{"Sign in from the header", "Register from the header"},
			"scenarios": []map[string]any{{
				"name": "Logged out", "given": "I am signed out",
				"when": "I view the header", "then": "I see Login and Register",
			}},
		},
	}, &env)

	if env.TicketType != TicketTypeStory {
		t.Fatalf("ticketType = %q, want story", env.TicketType)
	}
	card := env.card(t)
	uat, ok := card[SectionUAT].([]any)
	if !ok || len(uat) != 2 {
		t.Fatalf("uat did not persist: %v", card[SectionUAT])
	}
	scenarios, ok := card[SectionScenarios].([]any)
	if !ok || len(scenarios) != 1 {
		t.Fatalf("scenarios did not persist: %v", card[SectionScenarios])
	}
	// Both carry content, so both are reported as shaped, not omitted.
	if !hasString(env.Shaped, SectionUAT) || !hasString(env.Shaped, SectionScenarios) {
		t.Fatalf("shaped = %v, want uat and scenarios", env.Shaped)
	}
	if len(env.Omitted) != 0 {
		t.Fatalf("omitted = %v, want nothing left to offer", env.Omitted)
	}
}

// seedShapes=false means "store exactly what I sent" — for a caller that has
// its own opinion about the card.
func TestCreateTicketSeedShapesFalseLeavesCardEmpty(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, org, tenant, user)

	var env shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId":    board.ID,
		"title":      "Nothing shaped here",
		"seedShapes": false,
	}, &env)

	card := env.card(t)
	if len(card) != 0 {
		t.Fatalf("cardData = %v, want empty when seedShapes is false", card)
	}
	if len(env.Shaped) != 0 {
		t.Fatalf("shaped = %v, want none", env.Shaped)
	}
}

// The legacy type stays accepted and canonicalises to story, so callers written
// before CW-31 keep working.
func TestCreateTicketLegacyTypeAliasesToStory(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, org, tenant, user)

	var env shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId":    board.ID,
		"title":      "Written by an older client",
		"ticketType": "ticket",
	}, &env)

	if env.TicketType != TicketTypeStory {
		t.Fatalf("ticketType = %q, want ticket to alias to story", env.TicketType)
	}
}

// The catalog describes what the UI renders, not what may be stored. A key we
// have never heard of must survive a round trip untouched.
func TestCreateTicketKeepsUnknownCardDataKeys(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, org, tenant, user)

	var env shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId":  board.ID,
		"title":    "Has a key from the future",
		"cardData": map[string]any{"riskRegister": []string{"vendor lock-in"}},
	}, &env)

	card := env.card(t)
	got, ok := card["riskRegister"].([]any)
	if !ok || len(got) != 1 || got[0] != "vendor lock-in" {
		t.Fatalf("unknown key was stripped: %v", card)
	}
}

// The merge rule, and the reason it exists: adding a UAT must not silently
// destroy the story block the caller did not mention.
func TestUpdateTicketCardDataMergesRatherThanReplaces(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, org, tenant, user)

	var env shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId": board.ID,
		"title":   "Header links",
		"cardData": map[string]any{
			"story": map[string]any{"asA": "visitor", "iWant": "to sign in", "soThat": "I can see my board"},
		},
	}, &env)

	var updated shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "update_ticket", map[string]any{
		"id":       env.ID,
		"cardData": map[string]any{"uat": []string{"Sign in from the header"}},
	}, &updated)

	card := updated.card(t)
	story, ok := card[SectionStory].(map[string]any)
	if !ok {
		t.Fatalf("story block was wiped by an unrelated update: %v", card)
	}
	if story["asA"] != "visitor" {
		t.Fatalf("story.asA = %v, want it preserved", story["asA"])
	}
	uat, ok := card[SectionUAT].([]any)
	if !ok || len(uat) != 1 {
		t.Fatalf("uat did not land: %v", card[SectionUAT])
	}
}

// CW-35 chose hint-only over syncing cardData.uat into a checklist, which makes
// the hint the entire feature. It has to point at create_checklist in both
// directions: when a UAT is missing, and when one exists and wants ticking.
func TestHintPointsAtChecklistWhenUATExists(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, org, tenant, user)

	var withUAT shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId":  board.ID,
		"title":    "Has a UAT already",
		"cardData": map[string]any{"uat": []string{"Sign in from the header"}},
	}, &withUAT)
	if !containsSub(withUAT.Hint, "create_checklist") {
		t.Fatalf("hint should offer a checklist when a UAT exists: %q", withUAT.Hint)
	}

	var without shapeEnvelope
	mcpToolCall(t, h, org, tenant, user, "create_ticket", map[string]any{
		"boardId": board.ID,
		"title":   "No UAT yet",
	}, &without)
	if !containsSub(without.Hint, "create_checklist") {
		t.Fatalf("hint should offer a checklist when a UAT is missing: %q", without.Hint)
	}
}

func containsSub(s, sub string) bool {
	return len(s) >= len(sub) && strings.Contains(s, sub)
}

// cardData is tenant data like any other: another tenant must not see it, even
// holding the ticket's UUID.
func TestCardDataIsTenantIsolated(t *testing.T) {
	orgA, tenantA, userA := newTestUUID(), newTestUUID(), newTestUUID()
	h, board := shapesBoard(t, orgA, tenantA, userA)

	var env shapeEnvelope
	mcpToolCall(t, h, orgA, tenantA, userA, "create_ticket", map[string]any{
		"boardId":  board.ID,
		"title":    "Tenant A only",
		"cardData": map[string]any{"uat": []string{"secret step"}},
	}, &env)

	orgB, tenantB, userB := newTestUUID(), newTestUUID(), newTestUUID()
	params, err := json.Marshal(map[string]any{
		"name":      "get_ticket",
		"arguments": map[string]any{"id": env.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := mcpCall(t, h, orgB, tenantB, userB,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+string(params)+`}`)
	if body := rec.Body.String(); !isErrorOrEmpty(body) {
		t.Fatalf("tenant B could read tenant A's ticket: %s", body)
	}
}

// A cross-tenant read should fail; accept either an MCP error or a miss, since
// the store reports "not found" rather than leaking that the row exists.
func isErrorOrEmpty(body string) bool {
	var resp struct {
		Result struct {
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return true
	}
	return resp.Error != nil || resp.Result.IsError
}

// A new board carries the catalog so a client can discover the shapes without
// hardcoding them. Existing boards are left alone, which is why this only
// asserts on one it just created.
func TestNewBoardGetsDefaultCardSchema(t *testing.T) {
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	a := &app{store: testDB(t), logBuf: NewLogBuffer()}
	h := withCORS(a.routes(withTestClaims))

	var board BoardView
	mcpToolCall(t, h, org, tenant, user, "create_board",
		map[string]any{"name": "Fresh", "keyPrefix": "FRS"}, &board)

	schema := map[string]any{}
	if err := json.Unmarshal(board.CardSchema, &schema); err != nil {
		t.Fatalf("cardSchema not an object: %v (%s)", err, board.CardSchema)
	}
	sections, ok := schema["sections"].(map[string]any)
	if !ok {
		t.Fatalf("cardSchema has no sections: %v", schema)
	}
	for _, key := range catalogOrder {
		if _, ok := sections[key]; !ok {
			t.Fatalf("cardSchema missing section %q: %v", key, sections)
		}
	}
}
