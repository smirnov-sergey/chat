//go:build integration

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/repository"
)

func assertConversationList(t *testing.T, got []conversation.Conversation, want ...*conversation.Conversation) {
	t.Helper()
	if got == nil || len(got) != len(want) {
		t.Fatalf("list = %+v, want a non-nil slice of %d conversations", got, len(want))
	}
	for i, expected := range want {
		if got[i].ID != expected.ID || got[i].Type != expected.Type || !got[i].CreatedAt.Equal(expected.CreatedAt) {
			t.Fatalf("item %d = %+v, want %+v", i, got[i], expected)
		}
	}
}

func TestConversationRepositoryListByUser(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, initialIDs := conversationTestDB(t, ctx)
	ids := append([]int64(nil), initialIDs[:]...)
	now := time.Date(2026, 11, 21, 12, 0, 0, 123456000, time.UTC)
	for i := 3; i < 6; i++ {
		var id int64
		username := fmt.Sprintf("user_%d", i)
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (username, name, created_at, updated_at)
			VALUES ($1, $1, $2, $2) RETURNING id`, username, now).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	store := repository.NewConversationDirectStore(db)
	create := func(firstID, secondID int64, createdAt time.Time) *conversation.Conversation {
		t.Helper()
		c, err := store.Create(ctx, firstID, secondID, createdAt)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first := create(ids[0], ids[1], now)
	second := create(ids[0], ids[2], now)
	newest := create(ids[0], ids[3], now.Add(time.Hour))
	oldest := create(ids[0], ids[4], now.Add(-time.Hour))
	foreign := create(ids[1], ids[2], now.Add(3*time.Hour))
	repo := repository.NewConversationRepository(db)

	all, err := repo.ListByUser(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	assertConversationList(t, all, newest, second, first, oldest)

	otherUser, err := repo.ListByUser(ctx, ids[1])
	if err != nil {
		t.Fatal(err)
	}
	assertConversationList(t, otherUser, foreign, first)

	for _, userID := range []int64{ids[5], ids[5] + 1} {
		list, err := repo.ListByUser(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		assertConversationList(t, list)
	}

	assertConversationCounts(t, ctx, db, 5, 5, 10)
}

func TestConversationRepositoryListByUserErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	repo := repository.NewConversationRepository(db)
	canceled, stop := context.WithCancel(ctx)
	stop()
	got, err := repo.ListByUser(canceled, ids[0])
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result = (%+v, %v), want nil and context.Canceled", got, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err = repo.ListByUser(ctx, ids[0])
	if got != nil || err == nil || errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("result = (%+v, %v), want a storage error rather than an empty list", got, err)
	}
}
