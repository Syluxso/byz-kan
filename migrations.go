package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// CW-22: schema changes to tables that already exist.
//
// docs/SCHEMA.sql is entirely CREATE TABLE IF NOT EXISTS. That is correct for a
// fresh database and for brand new tables, and silently does nothing for a table
// that already exists — so adding a column or relaxing a NOT NULL never reaches
// a deployed database. The failure mode is nasty: startup succeeds and the first
// query touching the new column fails at runtime.
//
// Rules for this list:
//
//   - Append only. Never edit or reorder a shipped migration; add a new one.
//   - One concern per entry, with a name that says what it does.
//   - Prefer idempotent SQL (ADD COLUMN IF NOT EXISTS, DROP CONSTRAINT IF
//     EXISTS). Applied migrations are recorded and skipped, but idempotent
//     statements mean a half-recorded state is still recoverable by re-running.
//   - Each runs in its own transaction, so a failure leaves that migration
//     wholly unapplied rather than half-applied.
type migration struct {
	Name string
	SQL  string
}

// migrations are applied in order after SCHEMA.sql.
var migrations = []migration{
	{
		// CW-19: attachments were ticket-only. Generalize to any parent so a
		// file can hang off a board or an agent message too.
		//
		// Backfills existing rows before relaxing ticket_id, so no row is ever
		// left without a parent. The whole thing is one transaction.
		Name: "2026-08-attachments-parent-columns",
		SQL: `
ALTER TABLE kan.attachments ADD COLUMN IF NOT EXISTS parent_type VARCHAR(16);
ALTER TABLE kan.attachments ADD COLUMN IF NOT EXISTS parent_id   UUID;

UPDATE kan.attachments
   SET parent_type = 'ticket', parent_id = ticket_id
 WHERE parent_type IS NULL AND ticket_id IS NOT NULL;

ALTER TABLE kan.attachments ALTER COLUMN ticket_id DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_attachments_parent
    ON kan.attachments (parent_type, parent_id, created_at)
    WHERE deleted_at IS NULL;
`,
	},
}

const migrationsTable = `
CREATE TABLE IF NOT EXISTS kan.schema_migrations (
    name        TEXT PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// applyMigrations runs any migration not yet recorded, in order.
//
// A record table rather than bare idempotent SQL, decided deliberately: it makes
// the startup log say what actually changed, and it means a future migration
// that cannot be expressed idempotently (a backfill, a data fix) does not have
// to be re-run on every boot.
func applyMigrations(ctx context.Context, db *sql.DB, list []migration) error {
	if _, err := db.ExecContext(ctx, migrationsTable); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[string]bool{}
	rows, err := db.QueryContext(ctx, `SELECT name FROM kan.schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		applied[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	ran := 0
	for _, m := range list {
		if applied[m.Name] {
			continue
		}
		if err := applyOne(ctx, db, m); err != nil {
			// Loud and fatal to the caller: a service running against a schema
			// it does not match is worse than a service that will not start.
			return fmt.Errorf("migration %q: %w", m.Name, err)
		}
		log.Printf("migration applied: %s", m.Name)
		ran++
	}
	if ran == 0 {
		log.Printf("migrations: nothing to apply (%d known)", len(list))
	}
	return nil
}

// applyOne runs a migration and records it in the same transaction, so the two
// can never disagree.
func applyOne(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO kan.schema_migrations (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`,
		m.Name); err != nil {
		return err
	}
	return tx.Commit()
}
