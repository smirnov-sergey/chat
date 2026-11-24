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
)

func waitForConversationMessageLock(t *testing.T, ctx context.Context, db *sql.DB, blockerPID int) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("message writer did not wait for the conversation lock")
		case <-ticker.C:
		}
	}
}

func TestConversationMessageStoreCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	direct, err := repository.NewConversationDirectStore(db).Create(ctx, ids[0], ids[1], time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	personal, err := repository.NewConversationPersonalStore(db).Create(ctx, ids[0], time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	store := repository.NewConversationMessageStore(db)
	for _, pair := range [][2]int64{{direct.ID, ids[0]}, {direct.ID, ids[1]}, {personal.ID, ids[0]}} {
		before := time.Now().UTC().Truncate(time.Microsecond)
		message, err := store.Create(ctx, pair[0], pair[1], " Привет 👋\n ")
		if err != nil {
			t.Fatal(err)
		}
		if message.ConversationID != pair[0] || message.SenderUserID != pair[1] || message.Text != " Привет 👋\n " || message.CreatedAt.Before(before) || message.CreatedAt.After(time.Now().UTC()) {
			t.Fatalf("unexpected message: %+v", message)
		}
		assertConversationMessage(t, ctx, db, *message)
	}
	for _, pair := range [][2]int64{{direct.ID, ids[2]}, {personal.ID, ids[1]}, {9223372036854775807, ids[0]}, {0, ids[0]}, {direct.ID, 0}} {
		message, err := store.Create(ctx, pair[0], pair[1], "denied")
		if message != nil || !errors.Is(err, conversation.ErrNotFound) {
			t.Fatalf("result = (%+v, %v), want not found", message, err)
		}
	}
	// Bypass service validation to force an INSERT failure after locking and access checking.
	message, err := store.Create(ctx, direct.ID, ids[0], "invalid\x00text")
	if message != nil || err == nil {
		t.Fatalf("result = (%+v, %v), want insertion failure", message, err)
	}
	canceledCtx, cancelRequest := context.WithCancel(ctx)
	cancelRequest()
	message, err = store.Create(canceledCtx, direct.ID, ids[0], "canceled")
	if message != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result = (%+v, %v), want canceled", message, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("message count = %d, want 3", count)
	}
	message, err = store.Create(ctx, direct.ID, ids[0], "after rollback")
	if err != nil {
		t.Fatal(err)
	}
	assertConversationMessage(t, ctx, db, *message)
}

func TestConversationMessageStoreSerializesWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	store := repository.NewConversationMessageStore(db)
	direct, err := repository.NewConversationDirectStore(db).Create(ctx, ids[0], ids[1], time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	personal, err := repository.NewConversationPersonalStore(db).Create(ctx, ids[0], time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var id int64
	var pid int
	if err := tx.QueryRowContext(ctx, "SELECT id, pg_backend_pid() FROM conversations WHERE id = $1 FOR NO KEY UPDATE", direct.ID).Scan(&id, &pid); err != nil {
		t.Fatal(err)
	}
	first := conversation.Message{ConversationID: direct.ID, SenderUserID: ids[0], Text: "first", CreatedAt: time.Now().UTC()}
	if err := repository.NewConversationMessageRepository(tx).Create(ctx, &first); err != nil {
		t.Fatal(err)
	}
	type result struct {
		message *conversation.Message
		err     error
	}
	done := make(chan result, 1)
	go func() {
		message, err := store.Create(ctx, direct.ID, ids[1], "second")
		done <- result{message, err}
	}()
	waitForConversationMessageLock(t, ctx, db, pid)
	// Ordinary reads and a different conversation must complete while this lock is held.
	probeCtx, cancelProbe := context.WithTimeout(ctx, 3*time.Second)
	defer cancelProbe()
	page, err := repository.NewConversationMessageRepository(db).List(probeCtx, direct.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 0 {
		t.Fatal("uncommitted messages must not be visible")
	}
	other, err := store.Create(probeCtx, personal.ID, ids[0], "independent")
	if err != nil {
		t.Fatal(err)
	}
	assertConversationMessage(t, ctx, db, *other)
	select {
	case got := <-done:
		t.Fatalf("writer finished before lock release: %+v", got)
	default:
	}
	releasedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.message.ID <= other.ID || got.message.ID <= first.ID || got.message.CreatedAt.Before(releasedAt) {
			t.Fatalf("ID and timestamp must be assigned after waiting: %+v", got.message)
		}
		assertConversationMessage(t, ctx, db, *got.message)
		page, err = repository.NewConversationMessageRepository(db).List(ctx, direct.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Messages) != 2 || page.Messages[0].ID != got.message.ID || page.Messages[1].ID != first.ID {
			t.Fatal("history must contain both committed messages in descending order")
		}
	case <-ctx.Done():
		t.Fatal("writer did not finish after lock release")
	}
}

func TestConversationMessageStoreChecksAccessAfterWaiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	direct, err := repository.NewConversationDirectStore(db).Create(ctx, ids[0], ids[1], time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var id int64
	var pid int
	if err := tx.QueryRowContext(ctx, "SELECT id, pg_backend_pid() FROM conversations WHERE id = $1 FOR NO KEY UPDATE", direct.ID).Scan(&id, &pid); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM conversations_members WHERE conversation_id = $1 AND user_id = $2", direct.ID, ids[1]); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		message, err := repository.NewConversationMessageStore(db).Create(ctx, direct.ID, ids[1], "revoked")
		if message != nil {
			done <- errors.New("failed creation returned a message")
			return
		}
		done <- err
	}()
	waitForConversationMessageLock(t, ctx, db, pid)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, conversation.ErrNotFound) {
			t.Fatalf("error = %v, want not found after access revocation", err)
		}
	case <-ctx.Done():
		t.Fatal("writer did not finish")
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_messages").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("revoked participant must not write messages")
	}
}
