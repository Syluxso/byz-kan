# byz-kan — V1 Build Plan & Specifications

**Status:** Spec locked 2026-08-25  
**Language:** Go (match `byz-todos` style)  
**DB:** PostgreSQL schema `kan`  
**Auth:** byz_aim JWT (RS256, JWKS)  
**Transport V1:** REST only  
**MCP:** same binary, Streamable HTTP at `/mcp` (stateless)

This service is **not** byz-todos. Todos are system-wide / agent work items. Kanban is boards, swimlanes, and tickets.

---

## 1. Goals

1. Multi org + **required** tenant on every row.
2. Soft-delete everything; cascade when a board is soft-deleted.
3. Auth only via byz_aim Bearer tokens (user or OAuth client).
4. Agent-friendly: Grok connections, gearsos, future MCP tools using the same tokens.
5. Human keys like `KAN-123` without leaking existence across tenants.
6. Time logging that sums to the ticket.
7. Attachments via byz-files; remote files as links.
8. `card_data` / `card_schema` JSONB present for later Cardwalla; no Cardwalla logic in V1.
9. `is_published` is a **client filter**, not ACL.
10. Keep V1 boring: REST, no Kafka, no webhooks yet.

---

## 2. Non-goals (V1)

- Milestones / sprints
- Custom workflow engines beyond per-board states
- Webhooks / Kafka event bus (add ASAP after domain is stable)
- Embedded blob storage
- Full ACL inside this service (row scope = org + tenant; board membership is soft membership, agents still filter)
- Cardwalla UI field semantics (store JSON only)
- Merging with byz-todos

---

## 3. Auth & tenancy

### 3.1 JWT claims (from byz_aim)

Same pattern as byz-todos:

| Claim | Required | Use |
|-------|----------|-----|
| `organization_id` | yes | row filter |
| `tenant_id` | **yes for byz-kan** | row filter (reject if missing) |
| `user_id` or `sub` | preferred | `created_by`, ownership |
| `client_id` / `app_id` | optional | agent / OAuth client identity |

Reject requests missing `organization_id` or `tenant_id`.

### 3.2 Soft delete visibility

- Default lists: `deleted_at IS NULL`
- Soft-deleted rows: **only** `created_by == current user` may read (optional `?includeDeleted=1` for owner)
- Everyone else: treat as not found

### 3.3 `is_published`

Boolean on boards. Service stores it. Clients filter public vs private. Service does **not** hide unpublished boards from same-tenant members by default (tenant isolation is the hard boundary).

### 3.4 Board membership

`board_members` records who is on a board (`owner` | `member`).  
V1 rule: any user in the **same org+tenant** may create/list boards and tickets on boards in that tenant. Membership is for UI, watchers, and future tighter ACL — not a hard gate yet. Document this so agents know to query by tenant.

---

## 4. Ticket numbers & keys

### 4.1 What “max number per board” means

Each board owns an integer sequence starting at 1.

When you create a ticket on board `B` in tenant `T`:

1. Take `next = COALESCE(MAX(number), 0) + 1` for rows with that `board_id` + `tenant_id` (including soft-deleted, so numbers never reuse).
2. Store `number = next`.
3. Build `key = upper(key_prefix) || '-' || number` (e.g. `SHIP-42`).

That MAX is the “max number per board”: the highest ticket sequence value already used on that board for that tenant. It is **not** a product limit (no cap of N tickets). It is the counter used to mint the next human-readable id.

Prefer a small `kan.board_sequences` table (`board_id`, `tenant_id`, `last_number`) updated in the same transaction to avoid races under concurrent creates. Unique constraints still guard correctness.

### 4.2 Uniqueness (tenant isolation, no cross-tenant leakage)

| Field | Unique scope |
|-------|----------------|
| `tickets.number` | `(tenant_id, board_id, number)` |
| `tickets.key` | `(tenant_id, key)` |
| `boards.key_prefix` | `(tenant_id, key_prefix)` recommended so two boards in one tenant cannot share `ABC` |

Different tenants **may** both have `ABC-1`. Allocating keys never checks other tenants, so you never reveal “that key is taken elsewhere.”

### 4.3 Routes

Never one ambiguous path param.

```
GET  /api/v1/tickets/id/{uuid}
GET  /api/v1/tickets/key/{key}     # key is PREFIX-N, scoped to JWT tenant
PATCH/DELETE same pattern
```

Also allow nested forms if useful:

```
/api/v1/boards/{boardId}/tickets
/api/v1/boards/{boardId}/tickets/id/{uuid}
```

Lookup by key always applies `organization_id` + `tenant_id` from JWT.

### 4.4 Key prefix default

On board create:

1. If client sends `keyPrefix`, normalize (A–Z, 0–9, 2–8 chars, uppercase).
2. Else derive from board `name`: first 4 alphanumeric characters, uppercase. If fewer than 2 usable chars, default `BOARD`.
3. If collision within tenant, append or increment suffix (`SHIP`, `SHIP2`, …) or return 409 and ask client to choose.

User may PATCH `keyPrefix` later **only if** no tickets exist on that board (or only if you accept changing display of future tickets only — V1: **block prefix change after first ticket**).

---

## 5. Default states

On every new board, seed four states (same transaction as board insert):

| name | position | is_default |
|------|----------|------------|
| Backlog | 0 | true |
| In Progress | 1 | false |
| Testing | 2 | false |
| Completed | 3 | false |

New tickets land in the `is_default` state unless `stateId` is provided.

When a ticket’s state becomes one named `Completed` (case-insensitive) **or** client sets a flag, set `completed_at = now()`. When moved out, clear `completed_at`. V1 heuristic: state name `Completed` (the seeded one). Optional later: `states.marks_complete boolean`.

---

## 6. Domain model

Common columns on almost every table:

- `id UUID PK DEFAULT gen_random_uuid()`
- `organization_id UUID NOT NULL`
- `tenant_id UUID NOT NULL`
- `created_by UUID NULL`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `deleted_at TIMESTAMPTZ NULL`

### 6.1 boards

| column | type | notes |
|--------|------|-------|
| name | text not null | |
| description | text | |
| key_prefix | varchar(16) not null | uppercase |
| is_published | boolean not null default false | client filter |
| card_schema | jsonb not null default '{}' | Cardwalla later |
| settings | jsonb not null default '{}' | |

### 6.2 states

| column | type | notes |
|--------|------|-------|
| board_id | uuid not null | FK boards |
| name | text not null | |
| position | int not null | swimlane order |
| is_default | boolean not null default false | one per board |
| wip_limit | int null | optional |
| color | varchar(32) null | |

### 6.3 tickets

| column | type | notes |
|--------|------|-------|
| board_id | uuid not null | |
| state_id | uuid not null | |
| parent_ticket_id | uuid null | self-FK |
| number | int not null | sequence per board+tenant |
| key | text not null | `PREFIX-N` |
| title | text not null | |
| body | text null | |
| card_data | jsonb not null default '{}' | |
| ticket_type | varchar(32) not null default 'ticket' | `ticket` \| `defect` |
| priority | int not null default 0 | client-defined meaning |
| position | int not null default 0 | order within state |
| due_at | timestamptz null | |
| estimate_minutes | int null | |
| logged_minutes | int not null default 0 | cached sum of time_entries |
| completed_at | timestamptz null | |

### 6.4 board_members

| column | notes |
|--------|-------|
| board_id, user_id | unique pair |
| role | `owner` \| `member` |

Creator of board becomes `owner`.

### 6.5 ticket_assignees

| board-independent | ticket_id, user_id unique |

### 6.6 ticket_watchers

| ticket_id, user_id unique |

### 6.7 tags + ticket_tags

**tags:** org+tenant scoped; `name`, optional `kind` (`project` \| `label` \| other).  
**ticket_tags:** ticket_id + tag_id.  
Projects are tags with `kind = 'project'`, not a separate model.

### 6.8 comments

`ticket_id`, `body`, `created_by`

### 6.9 links

`ticket_id`, `url`, `title`, `link_type` (`related` \| `blocks` \| `remote_file` \| `other`)

### 6.10 attachments

No blobs. `ticket_id`, `file_id` (byz-files), optional `filename`, `content_type`, `size_bytes`.

### 6.11 checklists + checklist_items

**checklists:** `ticket_id`, `title`, `position`  
**checklist_items:** `checklist_id`, `title`, `is_done`, `position`

### 6.12 time_entries

Supports **start/stop** and/or **duration**:

| column | notes |
|--------|-------|
| ticket_id | |
| user_id | who logged |
| started_at | nullable |
| ended_at | nullable |
| minutes | not null; if start+end present, may be derived |
| note | text |

Rules:

- At least one of: (`minutes` > 0) or (`started_at` and `ended_at`).
- If start+end provided without minutes, set `minutes = ceil(extract(epoch from (ended_at - started_at))/60)`.
- On insert/update/soft-delete of time_entries, recompute `tickets.logged_minutes = SUM(minutes)` for non-deleted entries.

### 6.13 activity_events

Lightweight local audit (not Kafka):

`board_id` null, `ticket_id` null, `actor_id`, `action` (e.g. `ticket.created`, `ticket.moved`, `time.logged`), `payload jsonb`.

### 6.14 board_sequences

`board_id`, `tenant_id`, `last_number int not null default 0`  
PK or unique `(board_id, tenant_id)`.

---

## 7. Cascade soft-delete

When soft-deleting a **board**:

1. Set `boards.deleted_at`.
2. Soft-delete all `states`, `tickets`, `board_members` for that board.
3. Soft-delete dependent ticket children: comments, links, attachments, checklists, checklist_items, time_entries, ticket_assignees, ticket_watchers, ticket_tags for those tickets.
4. Write an activity event.

When soft-deleting a **ticket**: cascade to its comments, links, attachments, checklists/items, time_entries, assignees, watchers, tag links. Do **not** delete child tickets (`parent_ticket_id`); either null the parent link or leave them (V1: null `parent_ticket_id` on children).

Hard delete: not in V1.

---

## 8. REST API (V1)

Base: `/api/v1`  
Auth: `Authorization: Bearer <jwt>`  
JSON: camelCase response fields (match byz-todos style).

### 8.1 Health

- `GET /healthz`
- `GET /actuator/health`
- `GET /api/v1/kan/ping`

### 8.2 Boards

| Method | Path | Notes |
|--------|------|-------|
| GET | `/boards` | list tenant boards |
| POST | `/boards` | create + seed states + sequence + owner member |
| GET | `/boards/{id}` | |
| PATCH | `/boards/{id}` | name, description, isPublished, settings, cardSchema; keyPrefix only if no tickets |
| DELETE | `/boards/{id}` | soft-delete cascade |

Query: `?published=true|false`, `?includeDeleted=1` (owner only).

### 8.3 States

| Method | Path |
|--------|------|
| GET | `/boards/{boardId}/states` |
| POST | `/boards/{boardId}/states` |
| PATCH | `/states/{id}` |
| DELETE | `/states/{id}` | soft-delete; block if tickets still in state unless `?force=1` moves them to default |
| POST | `/boards/{boardId}/states/reorder` | body: ordered ids |

### 8.4 Tickets

| Method | Path |
|--------|------|
| GET | `/boards/{boardId}/tickets` | filter state, assignee, tag, q |
| POST | `/boards/{boardId}/tickets` | allocate number+key |
| GET | `/tickets/id/{uuid}` |
| GET | `/tickets/key/{key}` |
| PATCH | `/tickets/id/{uuid}` |
| DELETE | `/tickets/id/{uuid}` |
| POST | `/tickets/id/{uuid}/move` | `{ stateId, position }` |

List order: by state position, then ticket position, then created_at.

### 8.5 Assignees / watchers / members

Standard nested routes under ticket or board. PUT replace or POST/DELETE individual.

### 8.6 Tags

Tenant-scoped tag CRUD + `POST/DELETE /tickets/id/{id}/tags/{tagId}`.

### 8.7 Comments, links, attachments, checklists

Nested under ticket id routes. Attachments body references `fileId` from byz-files only.

### 8.8 Time entries

| Method | Path |
|--------|------|
| GET | `/tickets/id/{id}/time-entries` |
| POST | `/tickets/id/{id}/time-entries` |
| PATCH | `/time-entries/{id}` |
| DELETE | `/time-entries/{id}` |

### 8.9 Activity

`GET /tickets/id/{id}/activity`  
`GET /boards/{id}/activity`

### 8.10 Admin (match todos)

`/api/v1/admin/logs`, optional db viewer behind JWT.

---

## 9. Service layout (Go)

Mirror byz-todos for speed, then split if files grow:

```
byz-kan/
  main.go           # mux, lifecycle
  config.go
  auth.go           # JWT, CORS, claims (require tenant_id)
  httputil.go       # writeJSON, writeProblem
  logs.go
  store.go or store/
    schema.go       # init SQL
    boards.go
    states.go
    tickets.go
    time_entries.go
    ...
  handlers_*.go
  Makefile
  Jenkinsfile       # copy style from byz-todos
  start.sh
  docs/
  go.mod            # module github.com/Syluxso/byz-kan
```

Port default: pick free port (e.g. **8106** if todos is 8105).

Env:

- `BIND`, `PORT`
- `DB_URL`
- `IAM_JWKS_URL` (default same as todos)

DB init: `CREATE SCHEMA IF NOT EXISTS kan;` + tables in `store` init (same approach as todos) **or** Flyway/goose later. V1 may embed init SQL like todos.

---

## 10. MCP (post-V1, same binary)

- Add Streamable HTTP MCP at `/mcp` using official Go MCP SDK.
- Tools call the same store methods as REST (create ticket, list board, log time, move ticket).
- Auth: same Bearer JWT (OAuth client from byz_aim).
- Optional later: `byz-kan mcp --stdio` for local IDE launchers.
- Do **not** ship a second microservice that HTTP-wraps REST.

MCP is a live protocol (tool list + tool call), not a static OpenAPI dump.

---

## 11. Implementation phases

### Phase 0 — Repo skeleton
- go.mod, main healthz, auth middleware requiring org+tenant, empty store init
- README + this doc (done)

### Phase 1 — Boards & states
- Schema: boards, states, board_members, board_sequences
- CRUD boards, seed states, cascade soft-delete board

### Phase 2 — Tickets
- tickets table, number allocation, key routes, move, priority, parent_ticket_id, completed_at
- assignees, watchers

### Phase 3 — Collaboration
- comments, tags, links, attachments (file_id only), checklists

### Phase 4 — Time & activity
- time_entries + logged_minutes recompute
- activity_events on create/move/delete/time

### Phase 5 — Polish
- admin logs, Jenkinsfile, Makefile, start.sh
- basic integration tests against Postgres

### Phase 6 — MCP + events (after V1 stable)
- `/mcp` tools
- Kafka / byz-events publish

---

## 12. Acceptance checks (V1)

1. Request without `tenant_id` claim → 403.
2. Two tenants can both create `DEMO-1`; neither sees the other.
3. Creating a board yields four states; new ticket lands in Backlog.
4. Ticket keys are `PREFIX-N` with N monotonic per board+tenant; soft-deleted numbers not reused.
5. Soft-delete board hides tickets/states from non-owners.
6. Time entry with only minutes works; with start/end computes minutes; ticket `loggedMinutes` matches sum.
7. Attachment rejects missing `fileId` pattern; no local disk store.
8. `GET /tickets/key/FOO-1` only resolves inside caller tenant.
9. Priority accepts any int; service does not map labels.
10. Moving ticket to Completed sets `completedAt`; moving away clears it.

---

## 13. Open follow-ups (not blocking V1 code)

- Whether `states.marks_complete` is better than name heuristic.
- WIP limit enforcement (warn vs hard block).
- Board membership as hard ACL for Cardwalla public org later.
- gears `kan` CLI command shapes.

---

## 14. Reference decisions log

| Decision | Choice |
|----------|--------|
| Priority | integer; client meaning |
| completed_at | yes |
| watchers | yes |
| Ticket number uniqueness | per tenant + board |
| Ticket key uniqueness | per tenant |
| Time entries | start/stop and/or minutes |
| Soft-delete cascade | yes |
| key_prefix default | first 4 alnum of name, user editable until tickets exist |
| MCP | same binary later |
| Tenant | required |
| Files | byz-files |
| Todos | separate service |
