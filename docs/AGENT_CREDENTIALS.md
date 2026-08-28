# Agent credentials: what reaches byz-files, and what does not

Short version: **a byz-kan personal access token works on byz-kan and nowhere
else.** Uploading a file needs an IAM access token. The two look alike and are
not interchangeable.

Written after an agent-visible failure where `add_attachment` succeeded and the
upload behind it could never have.

## The two credentials

| | byz-kan PAT | IAM access token |
|---|---|---|
| Algorithm | HS256 | RS256 |
| Signed with | `KAN_PAT_SECRET`, known only to byz-kan | IAM's private key |
| Verifiable by | byz-kan only | any service with the IAM JWKS |
| Lifetime | 365 days (`handleCreatePAT`) | ~1h, renewable since CW-30 |
| `grant_type` claim | `personal_access_token` | absent |
| Works on byz-kan | yes | yes |
| Works on byz-files | **no, 401** | yes |

Decode any token's payload to tell them apart; the `alg` header and the
`grant_type` claim are decisive.

## Why byz-files rejects a PAT

byz-file-service is an OAuth2 resource server. `SecurityConfig` validates JWTs
against `IAM_JWKS_URL`, so it can only verify RS256 signatures from IAM. A PAT
is HS256 signed with a secret byz-files does not have and should not have.

```
POST https://api.byzantineapp.dev/files/api/v1/files
  Authorization: Bearer <byz-kan PAT>
-> 401
   WWW-Authenticate: Bearer error="invalid_token",
     error_description="... Signed JWT rejected: Another algorithm expected,
     or no matching key(s) found"
```

The identity inside the PAT is genuine: `handleCreatePAT` logs into IAM, reads
the real claims, and re-signs *those same claims* with HS256. `organization_id`,
`tenant_id` and `sub` are all present and correct. It is only the signature that
byz-files cannot check, which is why loosening byz-files is not the fix. The
HS256 boundary is what keeps a stolen year-long PAT confined to byz-kan.

byz-kan accepts both because `parseAnyToken` (auth.go) tries RS256 first and
falls back to HS256.

## What this means per client

**HTTP-capable agents (Claude Code, scripts, CI).** Connect over OAuth and use
the IAM access token you already hold, not a PAT. That token is accepted by
byz-files with no server change, and by byz-kan as well, so one credential
covers both. Renew it with `grant_type=refresh_token` against `/oauth/token`;
the refresh grant has been live since CW-30. Do not paste a PAT into anything
that needs to reach another byz service.

**MCP-only clients (Grok web and similar).** There is no upload path today.
byz-kan's MCP surface has no tool that accepts bytes, by design: CW-19 says kan
stores a pointer and never the blob. byz-files streams content itself and never
issues presigned URLs, so there is no direct-to-storage escape either. Attaching
a `fileId` that a human already uploaded is the only option until that decision
is made. See CW-28.

## Rules

- NEVER share `KAN_PAT_SECRET` with another service.
- NEVER proxy bytes to byz-files under a service identity. byz-files would see
  the proxy, not the tenant actor, and the proxy would inherit sole
  responsibility for tenant authorization. That is a confused deputy.
- NEVER mint long-lived IAM access tokens to avoid implementing refresh. Short
  access plus refresh is the durable path.
- A PAT is fine for byz-kan-only automation. Say so explicitly wherever one is
  issued, so nobody assumes it is a platform credential.

## Upload flow, once you hold an IAM token

```bash
# 1. bytes to byz-files. The multipart part name must be "file".
curl -X POST https://api.byzantineapp.dev/files/api/v1/files \
  -H "Authorization: Bearer $IAM_ACCESS_TOKEN" \
  -F "file=@adr-0001.md"
# -> 201 {"id":"<fileId>","name":...,"contentType":...,"sizeBytes":...}

# 2. point byz-kan at it (MCP add_attachment, or REST)
#    parentType: ticket | board | message
```

byz-files caps uploads at 50MB. An organization with no active row in
`files.storage_provider_configs` gets `400 No active storage config for
organization`; that is provisioning, not auth. See CW-38.

## Related

- `handlers_tokens.go` — PAT minting
- `auth.go` `parseAnyToken` — RS256 before HS256
- `oauth.go` — authorization_code and refresh_token grants
- byz-file-service `SecurityConfig`, `JwtToUuidConverter`, `FileController`
- CW-19 (kan stores pointers), CW-28 (this gap), CW-29 (phantom fileIds),
  CW-30 (refresh), CW-38 (storage provisioning)
