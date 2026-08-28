-- byz-kan V1 target schema
-- Schema name: kan
-- All domain tables: organization_id + tenant_id NOT NULL, soft delete via deleted_at

CREATE SCHEMA IF NOT EXISTS kan;

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid

-- ---------------------------------------------------------------------------
-- boards
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.boards (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    name             TEXT NOT NULL,
    description      TEXT,
    key_prefix       VARCHAR(16) NOT NULL,
    is_published     BOOLEAN NOT NULL DEFAULT false,
    card_schema      JSONB NOT NULL DEFAULT '{}'::jsonb,
    settings         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_boards_tenant_key_prefix_active
    ON kan.boards (tenant_id, upper(key_prefix))
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_boards_org_tenant
    ON kan.boards (organization_id, tenant_id)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- board_sequences — monotonic ticket numbers per board+tenant (never reuse)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.board_sequences (
    board_id     UUID NOT NULL REFERENCES kan.boards (id),
    tenant_id    UUID NOT NULL,
    last_number  INT NOT NULL DEFAULT 0,
    PRIMARY KEY (board_id, tenant_id)
);

-- ---------------------------------------------------------------------------
-- states (swimlanes)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.states (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    board_id         UUID NOT NULL REFERENCES kan.boards (id),
    name             TEXT NOT NULL,
    position         INT NOT NULL DEFAULT 0,
    is_default       BOOLEAN NOT NULL DEFAULT false,
    wip_limit        INT,
    color            VARCHAR(32),
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_states_board
    ON kan.states (board_id, position)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- tickets
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.tickets (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id    UUID NOT NULL,
    tenant_id          UUID NOT NULL,
    board_id           UUID NOT NULL REFERENCES kan.boards (id),
    state_id           UUID NOT NULL REFERENCES kan.states (id),
    parent_ticket_id   UUID REFERENCES kan.tickets (id),
    number             INT NOT NULL,
    key                TEXT NOT NULL,
    title              TEXT NOT NULL,
    body               TEXT,
    card_data          JSONB NOT NULL DEFAULT '{}'::jsonb,
    ticket_type        VARCHAR(32) NOT NULL DEFAULT 'ticket',
    priority           INT NOT NULL DEFAULT 0,
    position           INT NOT NULL DEFAULT 0,
    due_at             TIMESTAMPTZ,
    estimate_minutes   INT,
    logged_minutes     INT NOT NULL DEFAULT 0,
    completed_at       TIMESTAMPTZ,
    created_by         UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at         TIMESTAMPTZ,
    -- CW-31: 'ticket' is the pre-catalog alias for story; kept so old rows stay valid.
    CONSTRAINT chk_ticket_type CHECK (ticket_type IN ('ticket', 'story', 'defect', 'spike', 'chore'))
);

-- numbers never reuse: unique even among soft-deleted rows
CREATE UNIQUE INDEX IF NOT EXISTS uq_tickets_tenant_board_number
    ON kan.tickets (tenant_id, board_id, number);

CREATE UNIQUE INDEX IF NOT EXISTS uq_tickets_tenant_key
    ON kan.tickets (tenant_id, upper(key));

CREATE INDEX IF NOT EXISTS idx_tickets_board_state
    ON kan.tickets (board_id, state_id, position)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tickets_org_tenant
    ON kan.tickets (organization_id, tenant_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tickets_parent
    ON kan.tickets (parent_ticket_id)
    WHERE deleted_at IS NULL AND parent_ticket_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- board_members
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.board_members (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    board_id         UUID NOT NULL REFERENCES kan.boards (id),
    user_id          UUID NOT NULL,
    role             VARCHAR(16) NOT NULL DEFAULT 'member',
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    CONSTRAINT chk_board_member_role CHECK (role IN ('owner', 'member'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_board_members_active
    ON kan.board_members (board_id, user_id)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- ticket_assignees
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.ticket_assignees (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    ticket_id        UUID NOT NULL REFERENCES kan.tickets (id),
    user_id          UUID NOT NULL,
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ticket_assignees_active
    ON kan.ticket_assignees (ticket_id, user_id)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- ticket_watchers
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.ticket_watchers (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    ticket_id        UUID NOT NULL REFERENCES kan.tickets (id),
    user_id          UUID NOT NULL,
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ticket_watchers_active
    ON kan.ticket_watchers (ticket_id, user_id)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- tags (projects are kind = 'project')
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.tags (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    name             TEXT NOT NULL,
    kind             VARCHAR(32) NOT NULL DEFAULT 'label',
    color            VARCHAR(32),
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_tags_tenant_name_kind_active
    ON kan.tags (tenant_id, lower(name), kind)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS kan.ticket_tags (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    ticket_id        UUID NOT NULL REFERENCES kan.tickets (id),
    tag_id           UUID NOT NULL REFERENCES kan.tags (id),
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ticket_tags_active
    ON kan.ticket_tags (ticket_id, tag_id)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- comments
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.comments (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    ticket_id        UUID NOT NULL REFERENCES kan.tickets (id),
    body             TEXT NOT NULL,
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_comments_ticket
    ON kan.comments (ticket_id, created_at)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- links
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.links (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    ticket_id        UUID NOT NULL REFERENCES kan.tickets (id),
    url              TEXT NOT NULL,
    title            TEXT,
    link_type        VARCHAR(32) NOT NULL DEFAULT 'related',
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    CONSTRAINT chk_link_type CHECK (link_type IN ('related', 'blocks', 'remote_file', 'other'))
);

-- ---------------------------------------------------------------------------
-- attachments (byz-files file_id only)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.attachments (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    -- Legacy: ticket-only attachments predate parent_type/parent_id (CW-19).
    -- Nullable now; parent_* is the source of truth. Kept so existing rows and
    -- the old index remain valid.
    ticket_id        UUID REFERENCES kan.tickets (id),
    -- ticket | board | message
    parent_type      VARCHAR(16),
    parent_id        UUID,
    file_id          UUID NOT NULL,
    filename         TEXT,
    content_type     TEXT,
    size_bytes       BIGINT,
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

-- NOTE: the (parent_type, parent_id) index lives in the CW-19 migration, not
-- here. This file runs BEFORE migrations, so on a database that predates those
-- columns the index would reference columns that do not exist yet and abort
-- startup. Column definitions above are safe: they only apply to a fresh
-- CREATE TABLE, and existing databases get them from the migration.

CREATE INDEX IF NOT EXISTS idx_attachments_ticket
    ON kan.attachments (ticket_id)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- checklists
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.checklists (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    ticket_id        UUID NOT NULL REFERENCES kan.tickets (id),
    title            TEXT NOT NULL,
    position         INT NOT NULL DEFAULT 0,
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS kan.checklist_items (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    checklist_id     UUID NOT NULL REFERENCES kan.checklists (id),
    title            TEXT NOT NULL,
    is_done          BOOLEAN NOT NULL DEFAULT false,
    position         INT NOT NULL DEFAULT 0,
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_checklist_items_list
    ON kan.checklist_items (checklist_id, position)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- time_entries
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.time_entries (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    ticket_id        UUID NOT NULL REFERENCES kan.tickets (id),
    user_id          UUID NOT NULL,
    started_at       TIMESTAMPTZ,
    ended_at         TIMESTAMPTZ,
    minutes          INT NOT NULL,
    note             TEXT,
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    CONSTRAINT chk_time_minutes_nonneg CHECK (minutes >= 0)
);

CREATE INDEX IF NOT EXISTS idx_time_entries_ticket
    ON kan.time_entries (ticket_id)
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- activity_events (local audit; Kafka later)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.activity_events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    board_id         UUID,
    ticket_id        UUID,
    actor_id         UUID,
    action           TEXT NOT NULL,
    payload          JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
    -- no soft delete; append-only
);

CREATE INDEX IF NOT EXISTS idx_activity_ticket
    ON kan.activity_events (ticket_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_activity_board
    ON kan.activity_events (board_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- oauth (Grok / MCP PKCE — stores clients + one-time auth codes)
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.oauth_clients (
    client_id      TEXT PRIMARY KEY,
    redirect_uris  JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS kan.oauth_codes (
    code            TEXT PRIMARY KEY,
    client_id       TEXT NOT NULL,
    redirect_uri    TEXT NOT NULL,
    code_challenge  TEXT NOT NULL,
    access_token    TEXT NOT NULL,
    refresh_token   TEXT,
    expires_in      INT NOT NULL DEFAULT 3600,
    expires_at      TIMESTAMPTZ NOT NULL,
    used_at         TIMESTAMPTZ
);

-- ---------------------------------------------------------------------------
-- messages (CW-18 — shared agent/human thread, board- or ticket-scoped)
--
-- Deliberately separate from kan.comments. Comments are product discussion on
-- a ticket; messages are coordination between agents and humans, board-level
-- or ticket-level, and are pruned and surfaced independently.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS kan.messages (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL,
    tenant_id        UUID NOT NULL,
    board_id         UUID NOT NULL REFERENCES kan.boards (id),
    -- NULL means the board-level thread rather than a specific ticket.
    ticket_id        UUID REFERENCES kan.tickets (id),
    actor_type       VARCHAR(16) NOT NULL DEFAULT 'agent',
    -- Stable per participant so two Groks do not collide.
    actor_key        TEXT NOT NULL,
    display_name     TEXT NOT NULL,
    body             TEXT NOT NULL,
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    CONSTRAINT ck_messages_actor_type CHECK (actor_type IN ('user', 'agent'))
);

-- Board thread, oldest first.
CREATE INDEX IF NOT EXISTS idx_messages_board
    ON kan.messages (board_id, created_at)
    WHERE deleted_at IS NULL;

-- Ticket thread, oldest first.
CREATE INDEX IF NOT EXISTS idx_messages_ticket
    ON kan.messages (ticket_id, created_at)
    WHERE deleted_at IS NULL AND ticket_id IS NOT NULL;
