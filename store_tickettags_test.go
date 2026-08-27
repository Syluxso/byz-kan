package main

import (
	"context"
	"testing"
)

// CW-16: the board renders tag chips per card, so tags must arrive with the
// ticket list rather than needing a request per card.
func TestListTicketsIncludesTags(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	sc := scope{OrgID: org, TenantID: tenant, ActorID: user}

	board, err := st.CreateBoard(ctx, org, tenant, user, "Chips", "", "CHP", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	red := "#ff0000"
	mcp, err := st.CreateTag(ctx, sc, "mcp", "feature", red)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := st.CreateTag(ctx, sc, "auth", "feature", "")
	if err != nil {
		t.Fatal(err)
	}

	mk := func(title string, tags ...string) TicketView {
		tkt, err := st.CreateTicket(ctx, sc, board.ID, "", "", title, "", "ticket", 0, 0, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range tags {
			if err := st.AddTicketTag(ctx, sc, tkt.ID, tag); err != nil {
				t.Fatal(err)
			}
		}
		return tkt
	}

	both := mk("two tags", mcp.ID, auth.ID)
	one := mk("one tag", mcp.ID)
	bare := mk("no tags")

	list, err := st.ListTickets(ctx, sc, ListTicketsParams{BoardID: board.ID})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]TagView{}
	for _, tk := range list {
		got[tk.ID] = tk.Tags
	}

	if n := len(got[both.ID]); n != 2 {
		t.Fatalf("two-tag ticket returned %d tags", n)
	}
	// Ordered by name, so auth precedes mcp.
	if got[both.ID][0].Name != "auth" || got[both.ID][1].Name != "mcp" {
		t.Fatalf("tags not name-ordered: %+v", got[both.ID])
	}
	if n := len(got[one.ID]); n != 1 || got[one.ID][0].Name != "mcp" {
		t.Fatalf("one-tag ticket returned %+v", got[one.ID])
	}
	if got[one.ID][0].Color == nil || *got[one.ID][0].Color != red {
		t.Fatalf("tag colour not returned: %+v", got[one.ID][0])
	}
	if len(got[bare.ID]) != 0 {
		t.Fatalf("untagged ticket returned %+v", got[bare.ID])
	}

	// Single-ticket reads must agree with the list, by id and by key.
	byID, err := st.GetTicketByID(ctx, sc, both.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(byID.Tags) != 2 {
		t.Fatalf("GetTicketByID returned %d tags", len(byID.Tags))
	}
	byKey, err := st.GetTicketByKey(ctx, sc, both.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(byKey.Tags) != 2 {
		t.Fatalf("GetTicketByKey returned %d tags", len(byKey.Tags))
	}

	// Detaching removes it from the payload.
	if err := st.RemoveTicketTag(ctx, sc, one.ID, mcp.ID); err != nil {
		t.Fatal(err)
	}
	after, err := st.GetTicketByID(ctx, sc, one.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Tags) != 0 {
		t.Fatalf("detached tag still present: %+v", after.Tags)
	}
}

// Tags must not leak across tenants even though the join is by ticket id.
func TestListTicketsTagsTenantScoped(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	sc := scope{OrgID: org, TenantID: tenant, ActorID: user}

	board, err := st.CreateBoard(ctx, org, tenant, user, "Scoped", "", "SCP", false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tag, err := st.CreateTag(ctx, sc, "private", "feature", "")
	if err != nil {
		t.Fatal(err)
	}
	tkt, err := st.CreateTicket(ctx, sc, board.ID, "", "", "secret", "", "ticket", 0, 0, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddTicketTag(ctx, sc, tkt.ID, tag.ID); err != nil {
		t.Fatal(err)
	}

	other := scope{OrgID: org, TenantID: newTestUUID(), ActorID: user}
	list, err := st.ListTickets(ctx, other, ListTicketsParams{BoardID: board.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range list {
		if len(tk.Tags) != 0 {
			t.Fatalf("tags leaked across tenants: %+v", tk.Tags)
		}
	}
}

// An empty ticket set must not build a malformed array literal.
func TestAttachTagsEmptySet(t *testing.T) {
	st := testDB(t)
	if err := st.attachTags(context.Background(), scope{}, nil); err != nil {
		t.Fatalf("nil set: %v", err)
	}
	if err := st.attachTags(context.Background(), scope{}, []TicketView{}); err != nil {
		t.Fatalf("empty set: %v", err)
	}
}

func TestPgTextArray(t *testing.T) {
	cases := map[string]string{
		"":                    "{}",
		"a":                   `{"a"}`,
		"a,b":                 `{"a","b"}`,
		`quote"and\backslash`: `{"quote\"and\\backslash"}`,
	}
	for input, want := range cases {
		var vals []string
		if input != "" {
			vals = splitComma(input)
		}
		if got := pgTextArray(vals); got != want {
			t.Fatalf("pgTextArray(%q) = %s, want %s", input, got, want)
		}
	}
}

func splitComma(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}
