package main

import (
	"net/http"
	"testing"
)

func TestCollabReplaceAndTenantList(t *testing.T) {
	st := testDB(t)
	org, tenant, user := newTestUUID(), newTestUUID(), newTestUUID()
	h := testClient(t, st, org, tenant, user)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/boards", org, tenant, user, map[string]any{
		"name": "Collab", "keyPrefix": "COL",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("board %d %s", rec.Code, rec.Body.String())
	}
	board := decode[BoardView](t, rec)
	rec = doJSON(t, h, http.MethodPost, "/api/v1/boards/"+board.ID+"/tickets", org, tenant, user, map[string]any{
		"title": "Work item",
	})
	tkt := decode[TicketView](t, rec)

	u1, u2 := newTestUUID(), newTestUUID()
	rec = doJSON(t, h, http.MethodPut, "/api/v1/tickets/id/"+tkt.ID+"/assignees", org, tenant, user, map[string]any{
		"userIds": []string{u1, u2, u1},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("put assignees %d %s", rec.Code, rec.Body.String())
	}
	asg := decode[[]PersonLinkView](t, rec)
	if len(asg) != 2 {
		t.Fatalf("assignees=%d", len(asg))
	}
	rec = doJSON(t, h, http.MethodPut, "/api/v1/tickets/id/"+tkt.ID+"/assignees", org, tenant, user, map[string]any{
		"userIds": []string{u2},
	})
	asg = decode[[]PersonLinkView](t, rec)
	if len(asg) != 1 || asg[0].UserID != u2 {
		t.Fatalf("replace assignees %#v", asg)
	}

	rec = doJSON(t, h, http.MethodPost, "/api/v1/tags", org, tenant, user, map[string]any{
		"name": "alpha", "kind": "project",
	})
	tag := decode[TagView](t, rec)
	rec = doJSON(t, h, http.MethodPost, "/api/v1/tickets/id/"+tkt.ID+"/tags/"+tag.ID, org, tenant, user, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("add tag %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodPost, "/api/v1/tickets/id/"+tkt.ID+"/comments", org, tenant, user, map[string]any{
		"body": "hello",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, h, http.MethodPost, "/api/v1/tickets/id/"+tkt.ID+"/links", org, tenant, user, map[string]any{
		"url": "https://example.com/a", "title": "A", "linkType": "related",
	})
	link := decode[LinkView](t, rec)
	title := "B"
	rec = doJSON(t, h, http.MethodPatch, "/api/v1/links/"+link.ID, org, tenant, user, map[string]any{
		"title": title, "linkType": "blocks",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch link %d %s", rec.Code, rec.Body.String())
	}
	link = decode[LinkView](t, rec)
	if link.LinkType != "blocks" || deref(link.Title) != "B" {
		t.Fatalf("link %#v", link)
	}

	rec = doJSON(t, h, http.MethodPost, "/api/v1/tickets/id/"+tkt.ID+"/checklists", org, tenant, user, map[string]any{
		"title": "Do", "position": 0,
	})
	cl := decode[ChecklistView](t, rec)
	rec = doJSON(t, h, http.MethodPatch, "/api/v1/checklists/"+cl.ID, org, tenant, user, map[string]any{
		"title": "Done list",
	})
	cl = decode[ChecklistView](t, rec)
	if cl.Title != "Done list" {
		t.Fatalf("checklist title %s", cl.Title)
	}
	rec = doJSON(t, h, http.MethodPost, "/api/v1/checklists/"+cl.ID+"/items", org, tenant, user, map[string]any{
		"title": "step",
	})
	item := decode[ChecklistItemView](t, rec)
	done := true
	rec = doJSON(t, h, http.MethodPatch, "/api/v1/checklist-items/"+item.ID, org, tenant, user, map[string]any{
		"isDone": done,
	})
	item = decode[ChecklistItemView](t, rec)
	if !item.IsDone {
		t.Fatal("item not done")
	}

	rec = doJSON(t, h, http.MethodGet, "/api/v1/tickets?q=Work", org, tenant, user, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant list %d %s", rec.Code, rec.Body.String())
	}
	list := decode[[]TicketView](t, rec)
	if len(list) != 1 || list[0].ID != tkt.ID {
		t.Fatalf("tenant list %#v", list)
	}
	rec = doJSON(t, h, http.MethodGet, "/api/v1/tickets?q=nope", org, tenant, user, nil)
	list = decode[[]TicketView](t, rec)
	if len(list) != 0 {
		t.Fatalf("expected empty got %d", len(list))
	}
}
