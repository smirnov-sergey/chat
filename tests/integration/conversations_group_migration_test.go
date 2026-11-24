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
	"chat/internal/model/conversation"
	"chat/internal/repository"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestConversationsGroupMigration(t *testing.T) {
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
		"00006_create_conversations_personal.sql",
		"00007_create_conversations_messages.sql",
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
	now := time.Date(2026, 11, 24, 12, 0, 0, 123456000, time.UTC)
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
	personal, err := repository.NewConversationPersonalStore(db).Create(ctx, userIDs[0], now)
	if err != nil {
		t.Fatal(err)
	}
	message := conversation.Message{ConversationID: direct.ID, SenderUserID: userIDs[0], Text: "Привет!", CreatedAt: now}
	if err := repository.NewConversationMessageRepository(db).Create(ctx, &message); err != nil {
		t.Fatal(err)
	}
	if count, err := migrate.Up(ctx, db, files); err != nil || count == 0 {
		t.Fatalf("apply migrations = (%d, %v), want new migrations", count, err)
	}
	var groupCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_group").Scan(&groupCount); err != nil {
		t.Fatal(err)
	}
	if groupCount != 0 {
		t.Fatal("migration must not create groups automatically")
	}

	var groupIDs [3]int64
	for i := range groupIDs {
		if err := db.QueryRowContext(ctx, `
			INSERT INTO conversations (type, created_at) VALUES ('group', $1) RETURNING id`, now).Scan(&groupIDs[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO conversations_members (conversation_id, user_id) VALUES ($1, $2)`, groupIDs[i], userIDs[0]); err != nil {
			t.Fatal(err)
		}
	}
	const insertGroup = "INSERT INTO conversations_group (conversation_id, owner_user_id, name) VALUES ($1, $2, $3)"
	for _, id := range groupIDs[:2] {
		if _, err := db.ExecContext(ctx, insertGroup, id, userIDs[0], "Семья"); err != nil {
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
		{"duplicate conversation", insertGroup, []any{groupIDs[0], userIDs[0], "Другая группа"}, "23505", "conversations_group_pkey"},
		{"missing conversation", insertGroup, []any{nil, userIDs[0], "Семья"}, "23502", ""},
		{"missing name", insertGroup, []any{groupIDs[2], userIDs[0], nil}, "23502", ""},
		{"missing owner", insertGroup, []any{groupIDs[2], nil, "Семья"}, "23502", ""},
		{"unknown conversation", insertGroup, []any{int64(-1), userIDs[0], "Семья"}, "23503", ""},
		{"unknown owner", insertGroup, []any{groupIDs[2], int64(-1), "Семья"}, "23503", "conversations_group_owner_member_fkey"},
		{"owner belongs to another conversation", insertGroup, []any{groupIDs[2], userIDs[1], "Семья"}, "23503", "conversations_group_owner_member_fkey"},
		{"remove owner membership", "DELETE FROM conversations_members WHERE conversation_id = $1 AND user_id = $2", []any{groupIDs[0], userIDs[0]}, "23503", "conversations_group_owner_member_fkey"},
		{"change owner to outsider", "UPDATE conversations_group SET owner_user_id = $2 WHERE conversation_id = $1", []any{groupIDs[0], userIDs[1]}, "23503", "conversations_group_owner_member_fkey"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, tt.query, tt.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.code ||
				(tt.constraint != "" && pgErr.ConstraintName != tt.constraint) {
				t.Fatalf("query error = %v, want PostgreSQL code %s, constraint %q", err, tt.code, tt.constraint)
			}
		})
	}
	if count, err := migrate.Up(ctx, db, files); err != nil || count != 0 {
		t.Fatalf("repeat migrations = (%d, %v), want (0, nil)", count, err)
	}
	assertConversationDirect(t, ctx, db, direct, userIDs[0], userIDs[1])
	assertConversationPersonal(t, ctx, db, personal, userIDs[0])
	page, err := repository.NewConversationMessageRepository(db).List(ctx, direct.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 {
		t.Fatalf("messages = %+v, want preserved message %+v", page.Messages, message)
	}
	storedMessage := page.Messages[0]
	if storedMessage.ID != message.ID || storedMessage.ConversationID != message.ConversationID ||
		storedMessage.SenderUserID != message.SenderUserID || storedMessage.Text != message.Text ||
		!storedMessage.CreatedAt.Equal(message.CreatedAt) {
		t.Fatalf("message = %+v, want %+v", storedMessage, message)
	}
	for _, id := range groupIDs[:2] {
		var name, kind string
		var ownerID int64
		var createdAt time.Time
		if err := db.QueryRowContext(ctx, `
			SELECT g.owner_user_id, g.name, c.type, c.created_at
			FROM conversations_group g JOIN conversations c ON c.id = g.conversation_id
			WHERE g.conversation_id = $1`, id).Scan(&ownerID, &name, &kind, &createdAt); err != nil {
			t.Fatal(err)
		}
		if name != "Семья" || ownerID != userIDs[0] || kind != "group" || !createdAt.Equal(now) {
			t.Fatal("repeat migration must preserve group data")
		}
	}
}
