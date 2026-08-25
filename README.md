# byz-kan

Multi-tenant Kanban service for the Byzantine stack.

- Auth: JWT from **byz_aim** (Bearer, RS256, JWKS)
- Scope: every row has `organization_id` + **required** `tenant_id`
- Soft-delete everywhere; cascade on board delete
- Human ticket keys (`PREFIX-N`) unique **per tenant**
- Files via **byz-files**; remote URLs via `links`
- REST in V1; MCP route planned on the same binary
- Completely separate from **byz-todos**

## Docs

| Doc | Purpose |
|-----|---------|
| [docs/BUILD_PLAN.md](docs/BUILD_PLAN.md) | Exhaustive V1 build plan, schema, API, rules |
| [docs/SCHEMA.sql](docs/SCHEMA.sql) | Target PostgreSQL schema (`kan` schema) |

## Status

Greenfield. Spec locked 2026-08-25. Implementation not started.

## Related

- Auth / OAuth clients: `Syluxso/byz_aim`
- Reference Go service style: `Syluxso/byz-todos`
- File storage: `Syluxso/byz-files`
- Empty product shell: this repo
- CLI surface later: `gears kan` in `Syluxso/gears`
- Public UI target: Cardwalla (Cardwala) on Byzantine
