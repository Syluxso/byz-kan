package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Brand describes an alternate public-facing identity for this byz-kan instance.
// When an incoming request's Host (or X-Forwarded-Host) matches a brand key,
// OAuth discovery, login pages, and IAM token minting all use brand-specific values.
type Brand struct {
	Name      string // Display name shown on the login page, e.g. "Cardwallah"
	IssuerURL string // Auth server base URL, e.g. "https://auth.cardwallah.com"
	PublicURL string // MCP/API public base URL, e.g. "https://mcp.cardwallah.com"
	JwksURI   string // JWKS endpoint; defaults to IssuerURL + "/.well-known/jwks.json"
	ClientID  string // IAM client ID for this brand; falls back to KAN_IAM_CLIENT_ID
}

type app struct {
	store       *Store
	logBuf      *LogBuffer
	jwks        keyfunc.Keyfunc
	publicURL   string
	iamURL      string
	iamClientID string
	patSecret   []byte // HS256 key for personal access tokens; set via KAN_PAT_SECRET (hex)
	httpc       *http.Client
	filesURL    string           // byz-file-service base, for read-through only (CW-39)
	brands      map[string]Brand // host → Brand, populated from KAN_BRANDS env var
	hub         *Hub             // live board event fan-out for SSE subscribers
}

func main() {
	addr := env("BIND", "0.0.0.0") + ":" + env("PORT", "8109")
	jwksURL := env("IAM_JWKS_URL", "https://iam.byzantineapp.dev/.well-known/jwks.json")
	dbURL := env("DB_URL", "postgres://db:db@127.0.0.1:5441/kan?sslmode=disable")
	publicURL := strings.TrimRight(env("KAN_PUBLIC_URL", "https://api.byzantineapp.dev/kan"), "/")
	iamURL := strings.TrimRight(env("IAM_URL", "https://iam.byzantineapp.dev"), "/")
	iamClientID := env("KAN_IAM_CLIENT_ID", env("IAM_CLIENT_ID", ""))
	filesURL := strings.TrimRight(env("BYZ_FILES_URL", "https://api.byzantineapp.dev/files"), "/")

	brands := parseBrands(env("KAN_BRANDS", ""))

	patSecret := decodePATSecret(env("KAN_PAT_SECRET", ""))

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
	hub := NewHub()
	store.hub = hub
	ctx0 := context.Background()
	if err := store.init(ctx0); err != nil {
		log.Fatalf("db init: %v", err)
	}
	if err := store.initOAuth(ctx0); err != nil {
		log.Fatalf("oauth db init: %v", err)
	}

	jwks, err := keyfunc.NewDefaultCtx(ctx0, []string{jwksURL})
	if err != nil {
		log.Fatalf("jwks: %v", err)
	}

	a := &app{
		store:       store,
		logBuf:      logBuf,
		jwks:        jwks,
		publicURL:   publicURL,
		iamURL:      iamURL,
		iamClientID: iamClientID,
		patSecret:   patSecret,
		httpc:       &http.Client{Timeout: 15 * time.Second},
		filesURL:    filesURL,
		brands:      brands,
		hub:         hub,
	}
	if iamClientID == "" {
		log.Printf("warning: KAN_IAM_CLIENT_ID unset — Grok OAuth login will fail until set")
	}
	// Resolve the advertised resource URL per request, so a 401 on a branded
	// host points at that brand's metadata rather than the Byzantine default.
	mux := a.routes(func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			withJWT(jwks, patSecret, a.resourcePublicURL(r), h)(w, r)
		}
	})

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

	mux.HandleFunc("GET /.well-known/oauth-protected-resource", a.handlePRM)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", a.handlePRM)
	mux.HandleFunc("GET /mcp/.well-known/oauth-protected-resource", a.handlePRM)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", a.handleASMeta)
	mux.HandleFunc("POST /oauth/register", a.handleOAuthRegister)
	mux.HandleFunc("GET /oauth/authorize", a.handleOAuthAuthorize)
	mux.HandleFunc("POST /oauth/authorize", a.handleOAuthAuthorizePost)
	mux.HandleFunc("POST /oauth/token", a.handleOAuthToken)
	mux.HandleFunc("POST /api/v1/me/tokens", a.handleCreatePAT)

	mcpH := a.mcpHTTPHandler()
	mux.Handle("/mcp", j(mcpH.ServeHTTP))
	mux.Handle("/mcp/", j(mcpH.ServeHTTP))

	mux.HandleFunc("GET /api/v1/boards", j(a.listBoards))
	mux.HandleFunc("POST /api/v1/boards", j(a.createBoard))
	mux.HandleFunc("GET /api/v1/boards/{id}", j(a.getBoard))
	mux.HandleFunc("PATCH /api/v1/boards/{id}", j(a.patchBoard))
	mux.HandleFunc("DELETE /api/v1/boards/{id}", j(a.deleteBoard))
	mux.HandleFunc("GET /api/v1/boards/{id}/activity", j(a.boardActivity))
	mux.HandleFunc("GET /api/v1/boards/{boardId}/events", j(a.boardEvents))
	mux.HandleFunc("GET /api/v1/boards/{boardId}/messages", j(a.listBoardMessages))
	mux.HandleFunc("POST /api/v1/boards/{boardId}/messages", j(a.createBoardMessage))
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
	mux.HandleFunc("GET /api/v1/tickets/id/{id}/messages", j(a.listTicketMessages))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/messages", j(a.createTicketMessage))
	mux.HandleFunc("PATCH /api/v1/messages/{id}", j(a.patchMessage))
	mux.HandleFunc("DELETE /api/v1/messages/{id}", j(a.deleteMessage))

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

	mux.HandleFunc("GET /api/v1/tickets/id/{id}/attachments", j(a.listTicketAttachments))
	mux.HandleFunc("POST /api/v1/tickets/id/{id}/attachments", j(a.createTicketAttachment))
	mux.HandleFunc("GET /api/v1/boards/{boardId}/attachments", j(a.listBoardAttachments))
	mux.HandleFunc("POST /api/v1/boards/{boardId}/attachments", j(a.createBoardAttachment))
	mux.HandleFunc("GET /api/v1/messages/{id}/attachments", j(a.listMessageAttachments))
	mux.HandleFunc("POST /api/v1/messages/{id}/attachments", j(a.createMessageAttachment))
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

// parseBrands unmarshals KAN_BRANDS JSON into a host→Brand map.
// JwksURI defaults to IssuerURL + "/.well-known/jwks.json" when blank.
// Example env value:
//
//	{"mcp.cardwallah.com":{"Name":"Cardwallah","IssuerURL":"https://auth.cardwallah.com","PublicURL":"https://mcp.cardwallah.com"}}
func parseBrands(raw string) map[string]Brand {
	if raw == "" {
		return nil
	}
	var m map[string]Brand
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		log.Printf("warning: KAN_BRANDS parse error: %v", err)
		return nil
	}
	for host, b := range m {
		if b.JwksURI == "" {
			b.JwksURI = strings.TrimRight(b.IssuerURL, "/") + "/.well-known/jwks.json"
			m[host] = b
		}
	}
	return m
}
