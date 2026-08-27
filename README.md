# byz-kan

Multi-tenant Kanban service for the Byzantine stack. **Not** byz-todos.

| Setting | Default |
|---------|---------|
| HTTP | `8109` |
| DB | `postgres://db:db@127.0.0.1:5441/kan` |
| Auth | IAM JWT — `organization_id` **and** `tenant_id` required |
| Schema | `kan` |

- Soft-delete everywhere; cascade on board delete
- Human ticket keys (`PREFIX-N`) unique **per tenant**
- Files via **byz-file-service** `fileId`; remote URLs via `links`
- REST plus Streamable HTTP MCP at `/mcp` (same JWT)

## Run

```bash
export PORT=8109
export DB_URL=postgres://db:db@127.0.0.1:5441/kan?sslmode=disable
export IAM_JWKS_URL=https://iam.byzantineapp.dev/.well-known/jwks.json
make build
./byz-kan
```

Local Postgres: `projects/db/docker-compose.yml` service `byz-kan-db` (host port 5441).

Health: `GET /healthz` → `{ "status": "UP", "tickets": N }`  
Ping: `GET /api/v1/kan/ping`  
Gateway: `https://api.byzantineapp.dev/kan/**` → this service (`BYZ_KAN_URI`, default `http://127.0.0.1:8109`)

MCP (Grok Custom connector): `https://api.byzantineapp.dev/kan/mcp`  
Grok will prompt for OAuth (PKCE). After deploy, either let Grok discover metadata, or fill:

- Authorization: `https://api.byzantineapp.dev/kan/oauth/authorize`
- Token: `https://api.byzantineapp.dev/kan/oauth/token`
- Client ID: `grok` (or let Grok register)
- Token auth method: none (PKCE)
- Sign in with a Byzantine user whose JWT has `organization_id` **and** `tenant_id`

Supervisor must set `KAN_IAM_CLIENT_ID` (e.g. `byz-admin`).

Tools: `list_boards`, `create_board`, `list_tickets`, `create_ticket`, `get_ticket`, `move_ticket`, `log_time`.

## API (all under `/api/v1`, JWT except ping/health)

| Method | Path |
|--------|------|
| GET | `/boards` `?published=` `?includeDeleted=1` |
| POST/GET/PATCH/DELETE | `/boards`, `/boards/{id}` |
| GET/POST | `/boards/{boardId}/states` |
| POST | `/boards/{boardId}/states/reorder` |
| PATCH/DELETE | `/states/{id}` (`?force=1` moves tickets) |
| GET/POST | `/boards/{boardId}/tickets` |
| GET | `/tickets` tenant-wide `?boardId=&stateId=&assignee=&tagId=&q=` |
| GET/PATCH/DELETE | `/tickets/id/{id}` and `/tickets/key/{key}` |
| POST | `/tickets/id/{id}/move` |
| GET/POST/PUT/DELETE | `/tickets/id/{id}/assignees` (`PUT { "userIds": [] }`) |
| GET/POST/PUT/DELETE | `/tickets/id/{id}/watchers` |
| GET/POST/PATCH/DELETE | `/tags` |
| POST/DELETE | `/tickets/id/{id}/tags/{tagId}` |
| GET/POST | `/tickets/id/{id}/comments` |
| PATCH/DELETE | `/comments/{id}` |
| GET/POST | `/tickets/id/{id}/links` |
| PATCH/DELETE | `/links/{id}` |
| GET/POST | `/tickets/id/{id}/attachments` (`fileId` required) |
| GET/POST | `/tickets/id/{id}/checklists` |
| PATCH/DELETE | `/checklists/{id}` |
| POST | `/checklists/{id}/items` |
| PATCH/DELETE | `/checklist-items/{id}` |
| GET/POST | `/tickets/id/{id}/time-entries` |
| PATCH/DELETE | `/time-entries/{id}` |
| GET | `/tickets/id/{id}/activity`, `/boards/{id}/activity` |
| GET | `/boards/{boardId}/events` — SSE live board stream |
| GET | `/tickets?boardId=&tag=mcp` — filter a board by tag name |
| GET | `/admin/logs`, `/admin/db/tables` |

### Live board updates (SSE)

`GET /api/v1/boards/{boardId}/events` streams board mutations as they happen
(`text/event-stream`), so an open board reflects other people's and agents' edits
without polling. Events are published from the store layer, so MCP mutations
stream identically to REST ones.

Emitted: `ticket.created|updated|moved|deleted`, `state.created|updated|deleted`,
`states.reordered`, `board.updated|deleted`. Envelope:

```json
{ "type": "ticket.moved", "boardId": "…", "ticketId": "…",
  "actorId": "…", "at": "2026-08-26T23:59:00Z", "payload": { "stateId": "…" } }
```

Notes:

- Browser `EventSource` cannot set an `Authorization` header — use a fetch-based
  SSE reader. A token in the query string is deliberately not accepted.
- Live-only: no replay/`Last-Event-ID`. Refetch the board on reconnect.
- Fan-out is in-process. Correct for the current single-process deploy; if
  byz-kan is ever scaled horizontally this must move to Postgres `LISTEN/NOTIFY`
  or each instance will only see its own writes.
- The handler sets `X-Accel-Buffering: no` and flushes per event, but the
  **gateway must also not buffer this path** or events arrive batched or never.

## Schema changes

`docs/SCHEMA.sql` runs at startup and is entirely `CREATE TABLE IF NOT EXISTS`.
That creates anything missing and **silently does nothing to a table that
already exists** — so adding a column there never reaches a deployed database,
and the service starts fine and then fails on the first query.

New table: add it to `SCHEMA.sql`. Changing an existing table: add a migration
to the `migrations` slice in `migrations.go`.

```go
var migrations = []migration{
    {
        Name: "2026-08-attachments-parent-columns",
        SQL:  `ALTER TABLE kan.attachments ADD COLUMN IF NOT EXISTS parent_type TEXT`,
    },
}
```

Rules:

- **Append only.** Never edit or reorder a shipped migration; add a new one.
- Applied migrations are recorded in `kan.schema_migrations` and skipped.
- Each runs in its own transaction, recorded in the same transaction, so a
  failure leaves it wholly unapplied rather than half-applied.
- Prefer idempotent SQL (`ADD COLUMN IF NOT EXISTS`) anyway.
- A failing migration aborts startup. A service running against a schema it
  does not match is worse than one that will not start.

`TestRealMigrationsApply` runs the shipped list against a live database, so a
bad migration fails in tests rather than on deploy.

## Tests

```bash
go test ./...          # unit + Postgres integration (skips if :5441 is down)
```

V1 acceptance checks live in `integration_test.go` (two tenants, keys, cascade, time, attachments).

## Docs

| Doc | Purpose |
|-----|---------|
| [docs/BUILD_PLAN.md](docs/BUILD_PLAN.md) | V1 build plan, schema, API, rules |
| [docs/SCHEMA.sql](docs/SCHEMA.sql) | Target PostgreSQL schema (`kan`) |
| [DEPLOY.md](DEPLOY.md) | Supervisor / Jenkins / gateway |

## Related

- Auth: `byz-iam` (`iam.byzantineapp.dev`)
- Style: `byz-todos`
- Files: `byz-file-service`
- CLI later: `gears kan`
- UI later: Cardwalla

