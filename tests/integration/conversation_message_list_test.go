//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/repository"
)

func TestConversationMessageRepositoryList(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	now := time.Date(2026, 11, 23, 12, 0, 0, 123456000, time.UTC)
	direct, err := repository.NewConversationDirectStore(db).Create(ctx, ids[0], ids[1], now)
	if err != nil {
		t.Fatal(err)
	}
	personal, err := repository.NewConversationPersonalStore(db).Create(ctx, ids[0], now)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewConversationMessageRepository(db)
	for _, id := range []int64{direct.ID, personal.ID, -1} {
		page, err := repo.List(ctx, id, nil)
		if err != nil {
			t.Fatal(err)
		}
		if page.Messages == nil || len(page.Messages) != 0 || page.NextCursor != nil {
			t.Fatalf("expected empty page, got %+v", page)
		}
	}
	var messages []conversation.Message
	for i := 0; i < 105; i++ {
		message := conversation.Message{ConversationID: direct.ID, SenderUserID: ids[i%2], Text: fmt.Sprintf("Сообщение %d 👋", i), CreatedAt: now}
		if err := repo.Create(ctx, &message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
		// Interleaving another conversation creates gaps in this conversation's IDs.
		note := conversation.Message{ConversationID: personal.ID, SenderUserID: ids[0], Text: "Заметка", CreatedAt: now}
		if err := repo.Create(ctx, &note); err != nil {
			t.Fatal(err)
		}
		if i == 49 || i == 50 {
			page, err := repo.List(ctx, direct.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Messages) != 50 || (page.NextCursor != nil) != (i == 50) {
				t.Fatalf("unexpected page at %d messages: %+v", i+1, page)
			}
			if i == 50 && *page.NextCursor != messages[1].ID {
				t.Fatal("cursor must reference the last returned message, not the extra row")
			}
		}
	}

	var beforeID *int64
	var collected []conversation.Message
	for pageNumber, wantSize := range []int{50, 50, 5} {
		page, err := repo.List(ctx, direct.ID, beforeID)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Messages) != wantSize {
			t.Fatalf("page %d: size=%d, want %d", pageNumber, len(page.Messages), wantSize)
		}
		collected = append(collected, page.Messages...)
		if pageNumber < 2 {
			if page.NextCursor == nil || *page.NextCursor != page.Messages[wantSize-1].ID {
				t.Fatal("missing or incorrect continuation")
			}
		} else if page.NextCursor != nil {
			t.Fatal("last page must not have a continuation")
		}
		beforeID = page.NextCursor
		if pageNumber == 0 {
			message := conversation.Message{ConversationID: direct.ID, SenderUserID: ids[0], Text: "Новое сообщение", CreatedAt: now.Add(time.Second)}
			if err := repo.Create(ctx, &message); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i, got := range collected {
		want := messages[len(messages)-1-i]
		if got.ID != want.ID || got.ConversationID != want.ConversationID || got.SenderUserID != want.SenderUserID || got.Text != want.Text || !got.CreatedAt.Equal(want.CreatedAt) {
			t.Fatalf("message %d = %+v, want %+v", i, got, want)
		}
	}
	oldestID := messages[0].ID
	page, err := repo.List(ctx, direct.ID, &oldestID)
	if err != nil {
		t.Fatal(err)
	}
	if page.Messages == nil || len(page.Messages) != 0 || page.NextCursor != nil {
		t.Fatal("cursor at oldest message must return an empty page")
	}
	page, err = repo.List(ctx, personal.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 50 || page.NextCursor == nil {
		t.Fatal("personal history must use the same pagination")
	}
	for _, message := range page.Messages {
		if message.ConversationID != personal.ID || message.Text != "Заметка" {
			t.Fatal("history must not include another conversation's messages")
		}
	}
}

func TestConversationMessageRepositoryListErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, _ := conversationTestDB(t, ctx)
	repo := repository.NewConversationMessageRepository(db)
	canceledCtx, cancelRequest := context.WithCancel(ctx)
	cancelRequest()
	page, err := repo.List(canceledCtx, 1, nil)
	if page != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result = (%+v, %v), want canceled", page, err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	page, err = repository.NewConversationMessageRepository(tx).List(ctx, 1, nil)
	if page != nil || !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("result = (%+v, %v), want finished transaction", page, err)
	}
}
