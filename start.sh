#!/bin/bash
set -euo pipefail
# Port 8109 — do not use 8105 (managed-api), 8106 (alerts), 8107 (todos), 8108 (tags).
export PORT="${PORT:-8109}"
export BIND="${BIND:-127.0.0.1}"
export IAM_JWKS_URL="${IAM_JWKS_URL:-https://iam.byzantineapp.dev/.well-known/jwks.json}"
export IAM_URL="${IAM_URL:-https://iam.byzantineapp.dev}"
export KAN_PUBLIC_URL="${KAN_PUBLIC_URL:-https://api.byzantineapp.dev/kan}"
export DB_URL="${DB_URL:-postgres://db:db@127.0.0.1:5441/kan?sslmode=disable}"
# Required for Grok OAuth login (IAM public/confidential client id in your org):
# export KAN_IAM_CLIENT_ID=byz-admin

exec /opt/services/byz-kan/byz-kan
