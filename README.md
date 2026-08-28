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

## Card shapes

A card has two axes, and keeping them apart is the whole design (CW-31).

**Type** is the kind of work — one per ticket:

| Type | For |
|------|-----|
| `story` | Something a user wants. The default. |
| `defect` | Something broken. |
| `spike` | A question with a timebox. |
| `chore` | Work that just needs doing. |

`ticket` is still accepted as a legacy alias for `story`. Rows written before
the catalog carry it and are never rewritten; the Go layer canonicalises it on
write and the DB `CHECK` keeps allowing it.

**Sections** are optional shaped blocks in `cardData`. Any type may carry any
section — which is why UAT and scenarios are *not* types. A defect can have a
UAT too, and making them types would force a choice that does not exist.

| Section | Shape |
|---------|-------|
| `story` | `{asA, iWant, soThat}` |
| `acceptance` | `string[]` |
| `scenarios` | `[{name, given, when, then}]` |
| `uat` | `string[]` |
| `defect` | `{repro, expected, actual}` |
| `spike` | `{question, timeboxMinutes, approach, findings, outcome, followUp}` |
| `chore` | `{why, doneWhen}` |
| `source` | `agent` or `user` — who shaped the card |

The division of the record: **title** is the heading, **body** is evidence
(logs, snippets, history), **cardData** is the shaped blocks the UI renders.

### Seeding

`create_ticket` takes `ticketType`, `cardData` and `seedShapes` (default true).
With seeding on, the empty blocks for that type are filled in, so an agent can
create a usable card from a one-line request without interrogating the user
first:

| Type | Seeded | Offered, never auto-created |
|------|--------|------------------------------|
| `story` | `story`, `acceptance` | `scenarios`, `uat` |
| `defect` | `defect` | `scenarios`, `uat` |
| `spike` | `spike` (question from the title) | — |
| `chore` | `chore` | — |

Nothing seeds a story onto a chore or a UAT onto a spike. An empty block is an
invitation, and inviting the wrong thing is how cards fill with ceremony nobody
wanted. Sections the caller sends always win, and unknown keys are stored
untouched — the catalog describes what the UI renders, not what may be stored.

`seedShapes: false` stores only what the caller sent.

### Result envelope

`create_ticket` returns the ticket's own fields (so `.key` and `.id` still read
straight off it) plus:

```json
{ "shaped": ["story", "acceptance"],
  "omitted": ["scenarios", "uat"],
  "hint": "Shapes live on cardData. Add scenarios or uat with update_ticket, ..." }
```

`shaped` is what carries content; `omitted` is what is worth adding next. The
agent decides whether to ask the user or fill them in itself — the server states
what is missing rather than prescribing the conversation.

`update_ticket` **merges** `cardData`: keys you send replace, keys you omit
stay. A whole-object replace would mean adding a UAT silently destroyed the
story block, and the damage would only surface in the UI much later.

New boards are seeded with the catalog in `card_schema` so a client can discover
the shapes. Existing boards keep whatever they have.

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

