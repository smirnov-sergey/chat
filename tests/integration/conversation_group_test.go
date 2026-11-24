//go:build integration

package integration_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/repository"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestConversationGroupStoreCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	store := repository.NewConversationGroupStore(db)
	repo := repository.NewConversationRepository(db)
	now := time.Date(2026, 11, 24, 12, 0, 0, 123456000, time.UTC)
	memberIDs := []int64{ids[1], ids[0]}
	before := append([]int64(nil), memberIDs...)
	first, err := store.Create(ctx, "Семья", ids[0], memberIDs, now)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.ID <= 0 || first.Type != conversation.TypeGroup ||
		first.Name != "Семья" || first.OwnerUserID != ids[0] || !first.CreatedAt.Equal(now) {
		t.Fatalf("unexpected group: %+v", first)
	}
	if !reflect.DeepEqual(memberIDs, before) {
		t.Fatal("creation must not modify the supplied members")
	}
	for _, userID := range memberIDs {
		got, err := repo.GetGroupByIDForUser(ctx, first.ID, userID)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != first.ID || got.Type != first.Type || got.Name != first.Name ||
			got.OwnerUserID != first.OwnerUserID || !got.CreatedAt.Equal(first.CreatedAt) {
			t.Fatalf("group = %+v, want %+v", got, first)
		}
		common, err := repo.GetByIDForUser(ctx, first.ID, userID)
		if err != nil || common == nil || common.ID != first.ID || common.Type != conversation.TypeGroup {
			t.Fatalf("common lookup = (%+v, %v)", common, err)
		}
		list, err := repo.ListByUser(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		assertConversationList(t, list, &first.Conversation)
	}
	for _, pair := range [][2]int64{{first.ID, ids[2]}, {-1, ids[0]}} {
		got, err := repo.GetGroupByIDForUser(ctx, pair[0], pair[1])
		if got != nil || !errors.Is(err, conversation.ErrNotFound) {
			t.Fatalf("lookup = (%+v, %v), want nil and not found", got, err)
		}
	}
	list, err := repo.ListByUser(ctx, ids[2])
	if err != nil || len(list) != 0 {
		t.Fatalf("outsider list = (%+v, %v), want empty list", list, err)
	}
	second, err := store.Create(ctx, "Семья", ids[0], memberIDs, now)
	if err != nil || second == nil || second.ID == first.ID {
		t.Fatalf("repeat creation = (%+v, %v), want a new group", second, err)
	}
	assertConversationCounts(t, ctx, db, 2, 0, 4)
	var groups int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_group").Scan(&groups); err != nil {
		t.Fatal(err)
	}
	if groups != 2 {
		t.Fatalf("group count = %d, want 2", groups)
	}
	personal, err := repository.NewConversationPersonalStore(db).Create(ctx, ids[0], now)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetGroupByIDForUser(ctx, personal.ID, ids[0]); got != nil || !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("personal lookup = (%+v, %v), want nil and not found", got, err)
	}
}

func TestConversationGroupStoreRollback(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	store := repository.NewConversationGroupStore(db)
	for _, tt := range []struct {
		name    string
		members []int64
		code    string
	}{
		{"unknown member after owner", []int64{ids[0], -1}, "23503"},
		{"duplicate member", []int64{ids[0], ids[1], ids[1]}, "23505"},
		{"owner missing from members", []int64{ids[1], ids[2]}, "23503"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.Create(ctx, "Семья", ids[0], tt.members, time.Now().UTC())
			var pgErr *pgconn.PgError
			if got != nil || !errors.As(err, &pgErr) || pgErr.Code != tt.code {
				t.Fatalf("creation = (%+v, %v), want PostgreSQL code %s", got, err, tt.code)
			}
			assertConversationCounts(t, ctx, db, 0, 0, 0)
			var groups int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_group").Scan(&groups); err != nil {
				t.Fatal(err)
			}
			if groups != 0 {
				t.Fatal("failed creation must not leave group details")
			}
		})
	}
}

func TestConversationGroupErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	store := repository.NewConversationGroupStore(db)
	repo := repository.NewConversationRepository(db)
	canceled, stop := context.WithCancel(ctx)
	stop()
	got, err := store.Create(canceled, "Семья", ids[0], ids[:2], time.Now().UTC())
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("creation = (%+v, %v), want nil and context.Canceled", got, err)
	}
	got, err = repo.GetGroupByIDForUser(canceled, 1, ids[0])
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("lookup = (%+v, %v), want nil and context.Canceled", got, err)
	}
	assertConversationCounts(t, ctx, db, 0, 0, 0)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetGroupByIDForUser(ctx, 1, ids[0])
	if got != nil || err == nil || errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("lookup = (%+v, %v), want storage error", got, err)
	}
	got, err = store.Create(ctx, "Семья", ids[0], ids[:2], time.Now().UTC())
	if got != nil || err == nil {
		t.Fatalf("creation = (%+v, %v), want storage error", got, err)
	}
}
