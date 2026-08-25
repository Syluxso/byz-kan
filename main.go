package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type app struct {
	store  *Store
	logBuf *LogBuffer
}

func main() {
	addr := env("BIND", "0.0.0.0") + ":" + env("PORT", "8109")
	jwksURL := env("IAM_JWKS_URL", "https://iam.byzantineapp.dev/.well-known/jwks.json")
	dbURL := env("DB_URL", "postgres://db:db@127.0.0.1:5441/kan?sslmode=disable")

	logBuf := NewLogBuffer()
	teeStdLog(logBuf, "byz-kan")

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(time.Hour)

	store := NewStore(db)
	ctx0 := context.Background()
	if err := store.init(ctx0); err != nil {
		log.Fatalf("db init: %v", err)
	}

	jwks, err := keyfunc.NewDefaultCtx(ctx0, []string{jwksURL})
	if err != nil {
		log.Fatalf("jwks: %v", err)
	}

	a := &app{store: store, logBuf: logBuf}
	mux := a.routes(func(h http.HandlerFunc) http.HandlerFunc { return withJWT(jwks, h) })

	srv := &http.Server{
		Addr:              addr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(ctx0, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("byz-kan listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	n, err := a.store.CountActive(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "DOWN"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "UP", "tickets": n})
}

func (a *app) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"service": "byz-kan", "status": "ok"})
}

func (a *app) routes(j func(http.HandlerFunc) http.HandlerFunc) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealth)
	mux.HandleFunc("GET /actuator/health", a.handleHealth)
	mux.HandleFunc("GET /api/v1/kan/ping", a.handlePing)

	mux.HandleFunc("GET /api/v1/boards", j(a.listBoards))
	mux.HandleFunc("POST /api/v1/boards", j(a.createBoard))
	mux.HandleFunc("GET /api/v1/boards/{id}", j(a.getBoard))
	mux.HandleFunc("PATCH /api/v1/boards/{id}", j(a.patchBoard))
	mux.HandleFunc("DELETE /api/v1/boards/{id}", j(a.deleteBoard))
	mux.HandleFunc("GET /api/v1/boards/{id}/activity", j(a.boardActivity))
	mux.HandleFunc("GET /api/v1/boards/{id}/members", j(a.listMembers))
	mux.HandleFunc("POST /api/v1/boards/{id}/members", j(a.addMember))
	mux.HandleFunc("DELETE /api/v1/boards/{id}/members/{userId}", j(a.removeMember))

	mux.HandleFunc("GET /api/v1/boards/{boardId}/states", j(a.listStates))
	mux.HandleFunc("POST /api/v1/boards/{boardId}/states", j(a.createState))
	mux.HandleFunc("POST /api/v1/boards/{boardId}/states/reorder", j(a.reorderStates))
	mux.HandleFunc("PATCH /api/v1/states/{id}", j(a.patchState))
	mux.HandleFunc("DELETE /api/v1/states/{id}", j(a.deleteState))

	mux.HandleFunc("GET /api/v1/boards/{boardId}/tickets", j(a.listTickets))
	mux.HandleFunc("POST /api/v1/boards/{boardId}/tickets", j(a.createTicket))
	mux.HandleFunc("GET /api/v1/boards/{boardId}/tickets/id/{id}", j(a.getTicketOnBoard))
	mux.HandleFunc("PATCH /api/v1/boards/{boardId}/tickets/id/{id}", j(a.patchTicketOnBoard))
	mux.HandleFunc("DELETE /api/v1/boards/{boardId}/tickets/id/{id}", j(a.deleteTicketOnBoard))

	mux.HandleFunc("GET /api/v1/tickets", j(a.listTenantTickets))
	mux.HandleFunc("GET /api/v1/tickets/id/{id}", j(a.getTicket))
	mux.HandleFunc("GET /api/v1/tickets/key/{key}", j(a.getTicketByKey))
	mux.HandleFunc("PATCH /api/v1/tickets/id/{id}", j(a.patchTicket))
	mux.HandleFunc("DELETE /api/v1/tickets/id/{id}", j(a.deleteTicket))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/move", j(a.moveTicket))
	mux.HandleFunc("GET /api/v1/tickets/id/{id}/activity", j(a.ticketActivity))

	mux.HandleFunc("GET /api/v1/tickets/id/{id}/assignees", j(a.listAssignees))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/assignees", j(a.addAssignee))
	mux.HandleFunc("PUT /api/v1/tickets/id/{id}/assignees", j(a.replaceAssignees))
	mux.HandleFunc("DELETE /api/v1/tickets/id/{id}/assignees/{userId}", j(a.removeAssignee))
	mux.HandleFunc("GET /api/v1/tickets/id/{id}/watchers", j(a.listWatchers))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/watchers", j(a.addWatcher))
	mux.HandleFunc("PUT /api/v1/tickets/id/{id}/watchers", j(a.replaceWatchers))
	mux.HandleFunc("DELETE /api/v1/tickets/id/{id}/watchers/{userId}", j(a.removeWatcher))

	mux.HandleFunc("GET /api/v1/tags", j(a.listTags))
	mux.HandleFunc("POST /api/v1/tags", j(a.createTag))
	mux.HandleFunc("PATCH /api/v1/tags/{id}", j(a.patchTag))
	mux.HandleFunc("DELETE /api/v1/tags/{id}", j(a.deleteTag))
	mux.HandleFunc("GET /api/v1/tickets/id/{id}/tags", j(a.listTicketTags))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/tags/{tagId}", j(a.addTicketTag))
	mux.HandleFunc("DELETE /api/v1/tickets/id/{id}/tags/{tagId}", j(a.removeTicketTag))

	mux.HandleFunc("GET /api/v1/tickets/id/{id}/comments", j(a.listComments))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/comments", j(a.createComment))
	mux.HandleFunc("PATCH /api/v1/comments/{id}", j(a.patchComment))
	mux.HandleFunc("DELETE /api/v1/comments/{id}", j(a.deleteComment))

	mux.HandleFunc("GET /api/v1/tickets/id/{id}/links", j(a.listLinks))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/links", j(a.createLink))
	mux.HandleFunc("PATCH /api/v1/links/{id}", j(a.patchLink))
	mux.HandleFunc("DELETE /api/v1/links/{id}", j(a.deleteLink))

	mux.HandleFunc("GET /api/v1/tickets/id/{id}/attachments", j(a.listAttachments))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/attachments", j(a.createAttachment))
	mux.HandleFunc("DELETE /api/v1/attachments/{id}", j(a.deleteAttachment))

	mux.HandleFunc("GET /api/v1/tickets/id/{id}/checklists", j(a.listChecklists))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/checklists", j(a.createChecklist))
	mux.HandleFunc("PATCH /api/v1/checklists/{id}", j(a.patchChecklist))
	mux.HandleFunc("DELETE /api/v1/checklists/{id}", j(a.deleteChecklist))
	mux.HandleFunc("POST /api/v1/checklists/{id}/items", j(a.createChecklistItem))
	mux.HandleFunc("PATCH /api/v1/checklist-items/{id}", j(a.patchChecklistItem))
	mux.HandleFunc("DELETE /api/v1/checklist-items/{id}", j(a.deleteChecklistItem))

	mux.HandleFunc("GET /api/v1/tickets/id/{id}/time-entries", j(a.listTimeEntries))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/time-entries", j(a.createTimeEntry))
	mux.HandleFunc("PATCH /api/v1/time-entries/{id}", j(a.patchTimeEntry))
	mux.HandleFunc("DELETE /api/v1/time-entries/{id}", j(a.deleteTimeEntry))

	mux.HandleFunc("GET /api/v1/admin/db", j(a.handleAdminDB))
	mux.HandleFunc("GET /api/v1/admin/db/", j(a.handleAdminDB))
	mux.HandleFunc("GET /api/v1/admin/logs", j(a.handleAdminLogs))
	return mux
}

func (a *app) sc(w http.ResponseWriter, r *http.Request) (scope, bool) {
	c, ok := claimsFrom(r.Context())
	if !ok {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "missing claims")
		return scope{}, false
	}
	return scope{
		OrgID:    c.OrganizationID,
		TenantID: c.TenantID,
		ActorID:  c.OwnerUserID(),
		InclDel:  wantIncludeDeleted(r.URL.Query().Get("includeDeleted")),
	}, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	dec := jsonDecoder(r)
	if err := dec.Decode(dest); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid JSON")
		return false
	}
	return true
}
