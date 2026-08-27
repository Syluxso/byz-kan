package main

import (
	"context"
	"strings"
	"testing"
)

// CW-22 acceptance: a column added to a table that already exists actually
// arrives — the thing CREATE TABLE IF NOT EXISTS silently will not do.
func TestMigrationAddsColumnToExistingTable(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	table := "kan.mig_test_" + strings.ReplaceAll(newTestUUID()[:8], "-", "")

	if _, err := st.db.ExecContext(ctx, "CREATE TABLE "+table+" (id UUID PRIMARY KEY DEFAULT gen_random_uuid())"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })

	// Re-running the CREATE the way SCHEMA.sql would: a no-op that does NOT
	// bring the new column with it. This is the bug the ticket describes.
	if _, err := st.db.ExecContext(ctx,
		"CREATE TABLE IF NOT EXISTS "+table+" (id UUID PRIMARY KEY, note TEXT)"); err != nil {
		t.Fatal(err)
	}
	if columnExists(t, st, table, "note") {
		t.Fatal("CREATE TABLE IF NOT EXISTS added a column; the premise of CW-22 is wrong")
	}

	name := "test-add-note-" + table
	if err := applyMigrations(ctx, st.db, []migration{
		{Name: name, SQL: "ALTER TABLE " + table + " ADD COLUMN IF NOT EXISTS note TEXT"},
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.db.ExecContext(context.Background(),
			`DELETE FROM kan.schema_migrations WHERE name = $1`, name)
	})

	if !columnExists(t, st, table, "note") {
		t.Fatal("migration did not add the column")
	}
}

// Running twice is harmless, and the second run does no work.
func TestMigrationIsIdempotent(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	table := "kan.mig_twice_" + strings.ReplaceAll(newTestUUID()[:8], "-", "")

	if _, err := st.db.ExecContext(ctx, "CREATE TABLE "+table+" (id UUID PRIMARY KEY DEFAULT gen_random_uuid())"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })

	name := "test-twice-" + table
	list := []migration{{Name: name, SQL: "ALTER TABLE " + table + " ADD COLUMN IF NOT EXISTS note TEXT"}}
	t.Cleanup(func() {
		_, _ = st.db.ExecContext(context.Background(),
			`DELETE FROM kan.schema_migrations WHERE name = $1`, name)
	})

	for i := 0; i < 3; i++ {
		if err := applyMigrations(ctx, st.db, list); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	if !columnExists(t, st, table, "note") {
		t.Fatal("column missing after repeated runs")
	}

	var n int
	if err := st.db.QueryRowContext(ctx,
		`SELECT count(*) FROM kan.schema_migrations WHERE name = $1`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("recorded %d times, want 1", n)
	}
}

// A broken statement must fail loudly and leave nothing behind — not a
// half-changed table and not a record claiming it succeeded.
func TestMigrationFailsClosed(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	table := "kan.mig_bad_" + strings.ReplaceAll(newTestUUID()[:8], "-", "")

	if _, err := st.db.ExecContext(ctx, "CREATE TABLE "+table+" (id UUID PRIMARY KEY DEFAULT gen_random_uuid())"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })

	name := "test-bad-" + table
	err := applyMigrations(ctx, st.db, []migration{{
		Name: name,
		// First statement is fine, second is nonsense. The whole migration runs
		// in one transaction, so neither may survive.
		SQL: "ALTER TABLE " + table + " ADD COLUMN IF NOT EXISTS good TEXT;" +
			" ALTER TABLE " + table + " ADD COLUMN bad NOT_A_TYPE",
	}})
	if err == nil {
		t.Fatal("broken migration reported success")
	}
	if !strings.Contains(err.Error(), name) {
		t.Fatalf("error does not name the migration: %v", err)
	}
	if columnExists(t, st, table, "good") {
		t.Fatal("partial migration survived; the transaction did not roll back")
	}

	var n int
	if err := st.db.QueryRowContext(ctx,
		`SELECT count(*) FROM kan.schema_migrations WHERE name = $1`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("failed migration was recorded as applied")
	}
}

// Migrations run in the order given, so a later one may rely on an earlier one.
func TestMigrationsRunInOrder(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	table := "kan.mig_order_" + strings.ReplaceAll(newTestUUID()[:8], "-", "")

	if _, err := st.db.ExecContext(ctx, "CREATE TABLE "+table+" (id UUID PRIMARY KEY DEFAULT gen_random_uuid())"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+table) })

	a := "test-order-a-" + table
	b := "test-order-b-" + table
	t.Cleanup(func() {
		_, _ = st.db.ExecContext(context.Background(),
			`DELETE FROM kan.schema_migrations WHERE name = ANY($1)`, pgTextArray([]string{a, b}))
	})

	// The second depends on the column the first adds.
	if err := applyMigrations(ctx, st.db, []migration{
		{Name: a, SQL: "ALTER TABLE " + table + " ADD COLUMN IF NOT EXISTS note TEXT"},
		{Name: b, SQL: "UPDATE " + table + " SET note = 'x'"},
	}); err != nil {
		t.Fatalf("ordered apply: %v", err)
	}
	if !columnExists(t, st, table, "note") {
		t.Fatal("first migration did not apply")
	}
}

// The real migration list must apply cleanly against a live database — this is
// what catches a bad migration before it reaches a deploy.
func TestRealMigrationsApply(t *testing.T) {
	st := testDB(t)
	if err := applyMigrations(context.Background(), st.db, migrations); err != nil {
		t.Fatalf("shipped migrations failed: %v", err)
	}
}

func columnExists(t *testing.T, st *Store, qualified, column string) bool {
	t.Helper()
	parts := strings.SplitN(qualified, ".", 2)
	var n int
	err := st.db.QueryRowContext(context.Background(), `
SELECT count(*) FROM information_schema.columns
WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
`, parts[0], parts[1], column).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n > 0
}
