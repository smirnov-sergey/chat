//go:build integration

package integration_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	"chat/internal/migrate"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestConversationsMigration(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	files := os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))
	previous := fstest.MapFS{}
	for _, name := range []string{
		"00001_create_users.sql",
		"00002_add_users_password_hash.sql",
		"00003_create_sessions.sql",
		"00004_add_users_photo.sql",
	} {
		data, err := fs.ReadFile(files, name)
		if err != nil {
			t.Fatal(err)
		}
		previous[name] = &fstest.MapFile{Data: data}
	}
	if _, err := migrate.Up(ctx, db, previous); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 11, 20, 12, 0, 0, 123456000, time.UTC)
	var userIDs [3]int64
	for i, username := range []string{"ivan", "anna", "oleg"} {
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (username, name, password_hash, created_at, updated_at)
			VALUES ($1, $1, 'existing-password-hash', $2, $2) RETURNING id`, username, now).
			Scan(&userIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := migrate.Up(ctx, db, files); err != nil || count == 0 {
		t.Fatalf("apply migrations = (%d, %v), want new migrations", count, err)
	}
	var username, passwordHash string
	var createdAt, updatedAt time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT username, password_hash, created_at, updated_at FROM users WHERE id = $1`, userIDs[0]).
		Scan(&username, &passwordHash, &createdAt, &updatedAt); err != nil {
		t.Fatal(err)
	}
	if username != "ivan" || passwordHash != "existing-password-hash" ||
		!createdAt.Equal(now) || !updatedAt.Equal(now) {
		t.Fatal("migration must preserve the existing user")
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("migration must not create conversations for existing users")
	}

	const insertConversation = "INSERT INTO conversations (type, created_at) VALUES ($1, $2)"
	const insertMember = "INSERT INTO conversations_members (conversation_id, user_id) VALUES ($1, $2)"
	const insertPair = "INSERT INTO conversations_direct (conversation_id, low_user_id, high_user_id) VALUES ($1, $2, $3)"
	var conversationIDs [2]int64
	for i := range conversationIDs {
		if err := db.QueryRowContext(ctx, insertConversation+" RETURNING id", "direct", now).
			Scan(&conversationIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	first, second := conversationIDs[0], conversationIDs[1]
	if first <= 0 || second <= 0 || first == second {
		t.Fatal("expected distinct positive generated conversation IDs")
	}
	low, high, other := userIDs[0], userIDs[1], userIDs[2]
	if _, err := db.ExecContext(ctx, insertPair, first, low, high); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []int64{low, high} {
		if _, err := db.ExecContext(ctx, insertMember, first, userID); err != nil {
			t.Fatal(err)
		}
	}

	for _, tt := range []struct {
		name       string
		query      string
		args       []any
		code       string
		constraint string
	}{
		{"missing type", insertConversation, []any{nil, now}, "23502", ""},
		{"unknown type", insertConversation, []any{"unknown", now}, "23514", "conversations_type"},
		{"personal not implemented", insertConversation, []any{"personal", now}, "23514", "conversations_type"},
		{"group not implemented", insertConversation, []any{"group", now}, "23514", "conversations_type"},
		{"missing creation time", insertConversation, []any{"direct", nil}, "23502", ""},
		{"duplicate member", insertMember, []any{first, low}, "23505", "conversations_members_pkey"},
		{"missing member conversation", insertMember, []any{nil, low}, "23502", ""},
		{"missing member user", insertMember, []any{first, nil}, "23502", ""},
		{"unknown member conversation", insertMember, []any{int64(-1), low}, "23503", "conversations_members_conversation_id_fkey"},
		{"unknown member user", insertMember, []any{first, int64(-1)}, "23503", "conversations_members_user_id_fkey"},
		{"duplicate pair", insertPair, []any{second, low, high}, "23505", "conversations_direct_pair_unique"},
		{"duplicate pair conversation", insertPair, []any{first, low, other}, "23505", "conversations_direct_pkey"},
		{"self dialogue", insertPair, []any{second, low, low}, "23514", "conversations_direct_user_order"},
		{"reversed pair", insertPair, []any{second, high, low}, "23514", "conversations_direct_user_order"},
		{"missing pair conversation", insertPair, []any{nil, low, other}, "23502", ""},
		{"missing low user", insertPair, []any{second, nil, high}, "23502", ""},
		{"missing high user", insertPair, []any{second, low, nil}, "23502", ""},
		{"unknown pair conversation", insertPair, []any{int64(-1), low, other}, "23503", "conversations_direct_conversation_id_fkey"},
		{"unknown low user", insertPair, []any{second, int64(-1), high}, "23503", "conversations_direct_low_user_id_fkey"},
		{"unknown high user", insertPair, []any{second, low, other + 1}, "23503", "conversations_direct_high_user_id_fkey"},
		{"update to self dialogue", "UPDATE conversations_direct SET high_user_id = low_user_id WHERE conversation_id = $1", []any{first}, "23514", "conversations_direct_user_order"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Each case rolls back even if an invalid write unexpectedly succeeds.
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, tt.query, tt.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.code ||
				(tt.constraint != "" && pgErr.ConstraintName != tt.constraint) {
				t.Fatalf("write error = %v, want PostgreSQL code %s, constraint %q", err, tt.code, tt.constraint)
			}
		})
	}

	// A user may take part in multiple dialogues with different people.
	if _, err := db.ExecContext(ctx, insertPair, second, low, other); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []int64{low, other} {
		if _, err := db.ExecContext(ctx, insertMember, second, userID); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := migrate.Up(ctx, db, files); err != nil || count != 0 {
		t.Fatalf("repeat migrations = (%d, %v), want (0, nil)", count, err)
	}
	var conversationType string
	var storedLow, storedHigh int64
	if err := db.QueryRowContext(ctx, `
		SELECT c.type, c.created_at, d.low_user_id, d.high_user_id
		FROM conversations c JOIN conversations_direct d ON d.conversation_id = c.id
		WHERE c.id = $1`, first).Scan(&conversationType, &createdAt, &storedLow, &storedHigh); err != nil {
		t.Fatal(err)
	}
	if conversationType != "direct" || !createdAt.Equal(now) || storedLow != low || storedHigh != high {
		t.Fatal("repeat migration must preserve conversation and pair data")
	}
	rows, err := db.QueryContext(ctx, `
		SELECT conversation_id, user_id FROM conversations_members ORDER BY conversation_id, user_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantMembers := [][2]int64{{first, low}, {first, high}, {second, low}, {second, other}}
	i := 0
	for rows.Next() {
		var got [2]int64
		if err := rows.Scan(&got[0], &got[1]); err != nil {
			t.Fatal(err)
		}
		if i >= len(wantMembers) || got != wantMembers[i] {
			t.Fatalf("unexpected membership at row %d: %v", i, got)
		}
		i++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if i != len(wantMembers) {
		t.Fatalf("membership count = %d, want %d", i, len(wantMembers))
	}
}
