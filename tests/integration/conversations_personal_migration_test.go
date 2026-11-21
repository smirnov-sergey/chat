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
	"chat/internal/repository"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestConversationsPersonalMigration(t *testing.T) {
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
		"00005_create_conversations.sql",
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

	now := time.Date(2026, 11, 21, 12, 0, 0, 123456000, time.UTC)
	var userIDs [2]int64
	for i, username := range []string{"ivan", "anna"} {
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (username, name, created_at, updated_at)
			VALUES ($1, $1, $2, $2) RETURNING id`, username, now).Scan(&userIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	direct, err := repository.NewConversationDirectStore(db).Create(ctx, userIDs[0], userIDs[1], now)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := migrate.Up(ctx, db, files); err != nil || count == 0 {
		t.Fatalf("apply migrations = (%d, %v), want new migrations", count, err)
	}
	assertConversationDirect(t, ctx, db, direct, userIDs[0], userIDs[1])
	assertConversationCounts(t, ctx, db, 1, 1, 2)
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_personal").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("migration must not create personal conversations automatically")
	}

	var conversationIDs [2]int64
	for i := range conversationIDs {
		if err := db.QueryRowContext(ctx, `
			INSERT INTO conversations (type, created_at)
			VALUES ('personal', $1) RETURNING id`, now).Scan(&conversationIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	const insertPersonal = "INSERT INTO conversations_personal (conversation_id, user_id) VALUES ($1, $2)"
	if _, err := db.ExecContext(ctx, insertPersonal, conversationIDs[0], userIDs[0]); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name           string
		conversationID any
		userID         any
		code           string
		constraint     string
	}{
		{"duplicate owner", conversationIDs[1], userIDs[0], "23505", "conversations_personal_user_id_key"},
		{"duplicate conversation", conversationIDs[0], userIDs[1], "23505", "conversations_personal_pkey"},
		{"missing conversation", nil, userIDs[1], "23502", ""},
		{"missing owner", conversationIDs[1], nil, "23502", ""},
		{"unknown conversation", int64(-1), userIDs[1], "23503", "conversations_personal_conversation_id_fkey"},
		{"unknown owner", conversationIDs[1], int64(-1), "23503", "conversations_personal_user_id_fkey"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, insertPersonal, tt.conversationID, tt.userID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.code ||
				(tt.constraint != "" && pgErr.ConstraintName != tt.constraint) {
				t.Fatalf("insert error = %v, want PostgreSQL code %s, constraint %q", err, tt.code, tt.constraint)
			}
		})
	}
	if _, err := db.ExecContext(ctx, insertPersonal, conversationIDs[1], userIDs[1]); err != nil {
		t.Fatal(err)
	}
	for i, conversationID := range conversationIDs {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO conversations_members (conversation_id, user_id) VALUES ($1, $2)`, conversationID, userIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := migrate.Up(ctx, db, files); err != nil || count != 0 {
		t.Fatalf("repeat migrations = (%d, %v), want (0, nil)", count, err)
	}
	for i, conversationID := range conversationIDs {
		var ownerID int64
		var conversationType string
		var createdAt time.Time
		if err := db.QueryRowContext(ctx, `
			SELECT p.user_id, c.type, c.created_at
			FROM conversations_personal p JOIN conversations c ON c.id = p.conversation_id
			WHERE p.conversation_id = $1`, conversationID).Scan(&ownerID, &conversationType, &createdAt); err != nil {
			t.Fatal(err)
		}
		if ownerID != userIDs[i] || conversationType != "personal" || !createdAt.Equal(now) {
			t.Fatal("repeat migration must preserve personal conversation data")
		}
		var memberCount int
		if err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM conversations_members WHERE conversation_id = $1 AND user_id = $2`, conversationID, ownerID).Scan(&memberCount); err != nil {
			t.Fatal(err)
		}
		if memberCount != 1 {
			t.Fatal("repeat migration must preserve personal membership")
		}
	}
	assertConversationDirect(t, ctx, db, direct, userIDs[0], userIDs[1])
	assertConversationCounts(t, ctx, db, 3, 1, 4)
}
