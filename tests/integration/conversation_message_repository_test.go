//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/repository"

	"github.com/jackc/pgx/v5/pgconn"
)

func assertConversationMessage(t *testing.T, ctx context.Context, db *sql.DB, want conversation.Message) {
	t.Helper()
	var got conversation.Message
	if err := db.QueryRowContext(ctx, `
		SELECT id, conversation_id, sender_user_id, text, created_at
		FROM conversations_messages WHERE id = $1`, want.ID).
		Scan(&got.ID, &got.ConversationID, &got.SenderUserID, &got.Text, &got.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if got.ID <= 0 || got.ID != want.ID || got.ConversationID != want.ConversationID ||
		got.SenderUserID != want.SenderUserID || got.Text != want.Text || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("message = %+v, want %+v", got, want)
	}
}

func TestConversationMessageRepositoryCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	now := time.Date(2026, 11, 23, 15, 0, 0, 123456789, time.FixedZone("UTC+3", 3*60*60))
	direct, err := repository.NewConversationDirectStore(db).Create(ctx, ids[0], ids[1], now)
	if err != nil {
		t.Fatal(err)
	}
	personal, err := repository.NewConversationPersonalStore(db).Create(ctx, ids[0], now)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewConversationMessageRepository(db)
	for _, tt := range []struct {
		name           string
		conversationID int64
		senderUserID   int64
	}{
		{"direct first sender", direct.ID, ids[0]},
		{"direct second sender", direct.ID, ids[1]},
		{"personal", personal.ID, ids[0]},
	} {
		t.Run(tt.name, func(t *testing.T) {
			message := conversation.Message{
				ConversationID: tt.conversationID,
				SenderUserID:   tt.senderUserID,
				Text:           "  Привет! 👋\nВторая строка\t ",
				CreatedAt:      now,
			}
			if err := repo.Create(ctx, &message); err != nil {
				t.Fatal(err)
			}
			if message.ConversationID != tt.conversationID || message.SenderUserID != tt.senderUserID ||
				message.Text != "  Привет! 👋\nВторая строка\t " || !message.CreatedAt.Equal(now.Truncate(time.Microsecond)) {
				t.Fatalf("unexpected saved message: %+v", message)
			}
			assertConversationMessage(t, ctx, db, message)
		})
	}
}

func TestConversationMessageRepositoryCreateErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	now := time.Date(2026, 11, 23, 12, 0, 0, 123456789, time.UTC)
	personal, err := repository.NewConversationPersonalStore(db).Create(ctx, ids[0], now)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewConversationMessageRepository(db)
	for _, tt := range []struct {
		name           string
		conversationID int64
		senderUserID   int64
		constraint     string
	}{
		{"unknown conversation", -1, ids[0], "conversations_messages_conversation_id_fkey"},
		{"unknown sender", personal.ID, -1, "conversations_messages_sender_user_id_fkey"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			message := conversation.Message{ID: 42, ConversationID: tt.conversationID, SenderUserID: tt.senderUserID, Text: "text", CreatedAt: now}
			before := message
			err := repo.Create(ctx, &message)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23503" || pgErr.ConstraintName != tt.constraint {
				t.Fatalf("create error = %v, want foreign key violation %s", err, tt.constraint)
			}
			if message != before {
				t.Fatal("failed creation must not change the message")
			}
		})
	}
	t.Run("canceled context", func(t *testing.T) {
		canceledCtx, cancel := context.WithCancel(ctx)
		cancel()
		message := conversation.Message{ID: 42, ConversationID: personal.ID, SenderUserID: ids[0], Text: "text", CreatedAt: now}
		before := message
		if err := repo.Create(canceledCtx, &message); !errors.Is(err, context.Canceled) {
			t.Fatalf("create error = %v, want context.Canceled", err)
		}
		if message != before {
			t.Fatal("canceled creation must not change the message")
		}
	})
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("message count = %d, want 0", count)
	}
}

func TestConversationMessageRepositoryCreateTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	personal, err := repository.NewConversationPersonalStore(db).Create(ctx, ids[0], now)
	if err != nil {
		t.Fatal(err)
	}
	for _, commit := range []bool{false, true} {
		name := "rollback"
		if commit {
			name = "commit"
		}
		t.Run(name, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			repo := repository.NewConversationMessageRepository(tx)
			message := conversation.Message{ConversationID: personal.ID, SenderUserID: ids[0], Text: "text", CreatedAt: now}
			if err := repo.Create(ctx, &message); err != nil {
				t.Fatal(err)
			}
			if commit {
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
				assertConversationMessage(t, ctx, db, message)
			} else {
				if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}
				var count int
				if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages WHERE id = $1", message.ID).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Fatal("rolled back message must not be stored")
				}
			}
			before := message
			if err := repo.Create(ctx, &message); !errors.Is(err, sql.ErrTxDone) {
				t.Fatalf("create error = %v, want sql.ErrTxDone", err)
			}
			if message != before {
				t.Fatal("creation in a finished transaction must not change the message")
			}
		})
	}
}
