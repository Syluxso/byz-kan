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
Same Bearer IAM JWT (`organization_id` + `tenant_id`). Tools: `list_boards`, `create_board`, `list_tickets`, `create_ticket`, `get_ticket`, `move_ticket`, `log_time`.

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
| GET | `/admin/logs`, `/admin/db/tables` |

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

