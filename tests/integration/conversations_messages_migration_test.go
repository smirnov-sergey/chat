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

func TestConversationsMessagesMigration(t *testing.T) {
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

	now := time.Date(2026, 11, 23, 12, 0, 0, 123456000, time.UTC)
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
	if count, err := migrate.Up(ctx, db, files); err != nil || count == 0 {
		t.Fatalf("apply migrations = (%d, %v), want new migrations", count, err)
	}
	assertConversationDirect(t, ctx, db, direct, userIDs[0], userIDs[1])
	assertConversationPersonal(t, ctx, db, personal, userIDs[0])
	assertConversationCounts(t, ctx, db, 2, 1, 3)
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("migration must not create messages automatically")
	}

	const insertMessage = `INSERT INTO conversations_messages
		(conversation_id, sender_user_id, text, created_at) VALUES ($1, $2, $3, $4)`
	messages := []conversation.Message{
		{ConversationID: direct.ID, SenderUserID: userIDs[0], Text: "Привет! 👋\nВторая строка", CreatedAt: now},
		{ConversationID: direct.ID, SenderUserID: userIDs[1], Text: "Привет!", CreatedAt: now},
		{ConversationID: personal.ID, SenderUserID: userIDs[0], Text: "Заметка", CreatedAt: now},
	}
	for i := range messages {
		message := &messages[i]
		if err := db.QueryRowContext(ctx, insertMessage+" RETURNING id",
			message.ConversationID, message.SenderUserID, message.Text, message.CreatedAt).Scan(&message.ID); err != nil {
			t.Fatal(err)
		}
		if message.ID <= 0 || (i > 0 && message.ID <= messages[i-1].ID) {
			t.Fatal("sequential inserts must receive increasing positive IDs")
		}
	}

	for _, tt := range []struct {
		name           string
		conversationID any
		senderUserID   any
		text           any
		createdAt      any
		code           string
		constraint     string
	}{
		{"missing conversation", nil, userIDs[0], "text", now, "23502", ""},
		{"missing sender", direct.ID, nil, "text", now, "23502", ""},
		{"missing text", direct.ID, userIDs[0], nil, now, "23502", ""},
		{"missing date", direct.ID, userIDs[0], "text", nil, "23502", ""},
		{"unknown conversation", int64(-1), userIDs[0], "text", now, "23503", "conversations_messages_conversation_id_fkey"},
		{"unknown sender", direct.ID, int64(-1), "text", now, "23503", "conversations_messages_sender_user_id_fkey"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, insertMessage, tt.conversationID, tt.senderUserID, tt.text, tt.createdAt)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.code ||
				(tt.constraint != "" && pgErr.ConstraintName != tt.constraint) {
				t.Fatalf("insert error = %v, want PostgreSQL code %s, constraint %q", err, tt.code, tt.constraint)
			}
		})
	}

	t.Run("duplicate ID", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `INSERT INTO conversations_messages
			(id, conversation_id, sender_user_id, text, created_at)
			OVERRIDING SYSTEM VALUE VALUES ($1, $2, $3, $4, $5)`,
			messages[0].ID, direct.ID, userIDs[0], "duplicate", now)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "conversations_messages_pkey" {
			t.Fatalf("insert error = %v, want primary key violation", err)
		}
	})

	t.Run("membership removal preserves messages", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `DELETE FROM conversations_members
			WHERE conversation_id = $1 AND user_id = $2`, direct.ID, userIDs[0]); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages WHERE id = $1", messages[0].ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatal("removing membership must preserve the sender's message")
		}
	})

	if count, err := migrate.Up(ctx, db, files); err != nil || count != 0 {
		t.Fatalf("repeat migrations = (%d, %v), want (0, nil)", count, err)
	}
	for _, want := range messages {
		var got conversation.Message
		if err := db.QueryRowContext(ctx, `SELECT id, conversation_id, sender_user_id, text, created_at
			FROM conversations_messages WHERE id = $1`, want.ID).
			Scan(&got.ID, &got.ConversationID, &got.SenderUserID, &got.Text, &got.CreatedAt); err != nil {
			t.Fatal(err)
		}
		if got.ID != want.ID || got.ConversationID != want.ConversationID ||
			got.SenderUserID != want.SenderUserID || got.Text != want.Text || !got.CreatedAt.Equal(want.CreatedAt) {
			t.Fatalf("message = %+v, want %+v", got, want)
		}
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(messages) {
		t.Fatalf("message count = %d, want %d", count, len(messages))
	}
	assertConversationDirect(t, ctx, db, direct, userIDs[0], userIDs[1])
	assertConversationPersonal(t, ctx, db, personal, userIDs[0])
}
