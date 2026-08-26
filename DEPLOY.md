# Deploy byz-kan

Kanban API. Port **8109**. Schema `kan`. **Not** byz-todos.

## Prerequisites

1. Postgres database `kan` (local compose: `projects/db` service `byz-kan-db`, host **5441**)
2. IAM JWKS reachable
3. Gateway env `BYZ_KAN_URI=http://127.0.0.1:8109` (routes `/kan/**`)

## Env (supervisor)

```bash
export PORT=8109
export BIND=127.0.0.1
export DB_URL=postgres://kan:...@127.0.0.1:5432/kan?sslmode=disable
export IAM_JWKS_URL=https://iam.byzantineapp.dev/.well-known/jwks.json
```

Schema is created on process start (`CREATE SCHEMA IF NOT EXISTS kan` + tables).

| Item | Value |
|------|-------|
| Deploy dir | `/opt/services/byz-kan` |
| Binary | `byz-kan` |
| Supervisor | `byz-kan` |
| Health | `http://127.0.0.1:8109/healthz` |
| Gateway | `/kan/**` → strip prefix |

```bash
cat >/etc/supervisor/conf.d/byz-kan.conf <<'EOF'
[program:byz-kan]
command=/opt/services/byz-kan/start.sh
directory=/opt/services/byz-kan
autostart=true
autorestart=true
user=root
stdout_logfile=/var/log/supervisor/byz-kan.log
stderr_logfile=/var/log/supervisor/byz-kan.err.log
EOF
supervisorctl reread
supervisorctl update byz-kan
curl -sf http://127.0.0.1:8109/healthz
```

Jenkins copies the binary + `start.sh` and restarts supervisor (see `Jenkinsfile`).

Public URL (after byz-api-gateway is redeployed with `/kan`):

```bash
curl -s https://api.byzantineapp.dev/kan/healthz
curl -s https://api.byzantineapp.dev/kan/api/v1/kan/ping

Grok Custom connector URL: `https://api.byzantineapp.dev/kan/mcp` (Bearer JWT).
```

Admin System Health probes `https://api.byzantineapp.dev/kan/actuator/health`.

Do **not** bind 8105–8108 (managed-api, alerts, todos, tags).
