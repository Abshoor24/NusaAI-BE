package tests

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrationIntegration_0012PreservesActiveData(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	pool := requireMigrationTestDB(t)
	t.Cleanup(func() { cleanupMigrationObjects(t, pool) })

	migrateTo(t, dsn, 11)

	ctx, cancel := context.WithTimeout(context.Background(), testConnTimeout)
	defer cancel()

	userID := "11111111-1111-1111-1111-111111111111"
	projectID := "22222222-2222-2222-2222-222222222222"
	versionID := "33333333-3333-3333-3333-333333333333"
	attachmentID := "44444444-4444-4444-4444-444444444444"
	conversationID := "55555555-5555-5555-5555-555555555555"
	messageID := "66666666-6666-6666-6666-666666666666"
	messageAttachmentID := "77777777-7777-7777-7777-777777777777"
	documentID := "88888888-8888-8888-8888-888888888888"
	chunkID := "99999999-9999-9999-9999-999999999999"
	executionID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	embedding := "[" + strings.Repeat("0.1,", 767) + "0.1]"

	exec := func(name, sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}

	exec("user", `INSERT INTO users (id, email, password_hash)
		VALUES ($1, 'legacy-data@example.test', 'hash')`, userID)
	exec("project", `INSERT INTO projects (id, user_id, title, status, brief)
		VALUES ($1, $2, 'Active project', 'ready', '{"brand":"test"}'::jsonb)`, projectID, userID)
	exec("project version", `INSERT INTO project_versions
		(id, project_id, version_number, kind, prompt, html)
		VALUES ($1, $2, 1, 'generate', 'prompt', '<html>active</html>')`, versionID, projectID)
	exec("project attachment", `INSERT INTO project_attachments
		(id, project_id, file_name, file_type, file_size, storage_path, mime_type)
		VALUES ($1, $2, 'active.png', 'image/png', 123, 'uploads/active.png', 'image/png')`, attachmentID, projectID)

	exec("conversation", `INSERT INTO conversations (id, user_id, title)
		VALUES ($1, $2, 'Legacy conversation')`, conversationID, userID)
	exec("message", `INSERT INTO messages (id, conversation_id, role, content)
		VALUES ($1, $2, 'user', 'Legacy message')`, messageID, conversationID)
	exec("message attachment", `INSERT INTO message_attachments
		(id, message_id, file_name, file_type, file_size, storage_path, mime_type)
		VALUES ($1, $2, 'legacy.png', 'image/png', 456, 'uploads/legacy.png', 'image/png')`, messageAttachmentID, messageID)
	exec("document", `INSERT INTO documents
		(id, user_id, title, source, file_name, file_type, file_size, file_path, status, error_message)
		VALUES ($1, $2, 'Legacy document', 'upload', 'legacy.txt', 'text/plain', 789, 'uploads/legacy.txt', 'ready', NULL)`, documentID, userID)
	exec("document chunk", `INSERT INTO document_chunks
		(id, document_id, chunk_index, content, embedding)
		VALUES ($1, $2, 0, 'Legacy chunk', $3::vector)`, chunkID, documentID, embedding)
	exec("code execution", `INSERT INTO code_executions
		(id, user_id, language, code, stdin, stdout, stderr, exit_code, execution_time_ms, memory_kb, status)
		VALUES ($1, $2, 'go', 'package main', '', 'ok', '', 0, 12, 1024, 'success')`, executionID, userID)

	legacy := []string{
		"conversations", "messages", "message_attachments",
		"documents", "document_chunks", "code_executions",
	}
	active := []string{"users", "projects", "project_versions", "project_attachments"}
	before := countRows(ctx, pool, active)
	for _, table := range active {
		if before[table] != 1 {
			t.Fatalf("active table %s before 0012: got %d rows, want 1", table, before[table])
		}
	}
	t.Logf("active rows before 0012: %v", before)

	migrateTo(t, dsn, 12)
	assertLegacyTables(t, pool, legacy, false)
	assertRowCounts(t, pool, active, before)
	t.Logf("active rows after 0012-up: %v", countRows(ctx, pool, active))

	migrateTo(t, dsn, 11)
	assertLegacyTables(t, pool, legacy, true)
	empty := countRows(ctx, pool, legacy)
	for _, table := range legacy {
		if empty[table] != 0 {
			t.Fatalf("legacy table %s after 0012-down: got %d rows, want 0", table, empty[table])
		}
	}
	t.Logf("legacy rows after 0012-down: %v", empty)
	assertRowCounts(t, pool, active, before)
	t.Logf("active rows after 0012-down: %v", countRows(ctx, pool, active))

	migrateTo(t, dsn, 12)
	assertLegacyTables(t, pool, legacy, false)
	assertRowCounts(t, pool, active, before)
	t.Logf("active rows after 0012-up-again: %v", countRows(ctx, pool, active))
}

func assertLegacyTables(t *testing.T, pool *pgxpool.Pool, tables []string, wantPresent bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testConnTimeout)
	defer cancel()

	for _, table := range tables {
		var present bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = $1
		)`, table).Scan(&present); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if present != wantPresent {
			t.Fatalf("table %s present=%v, want %v", table, present, wantPresent)
		}
	}
}

func assertRowCounts(t *testing.T, pool *pgxpool.Pool, tables []string, want map[string]int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testConnTimeout)
	defer cancel()

	got := countRows(ctx, pool, tables)
	for _, table := range tables {
		if got[table] != want[table] {
			t.Fatalf("active table %s: got %d rows, want %d", table, got[table], want[table])
		}
	}
}
