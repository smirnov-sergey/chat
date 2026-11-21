//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/model/user"
	"chat/internal/repository"

	"github.com/jackc/pgx/v5/pgconn"
)

func assertConversationPersonal(t *testing.T, ctx context.Context, db *sql.DB, want *conversation.Conversation, userID int64) {
	t.Helper()
	got, err := repository.NewConversationRepository(db).GetPersonalByUser(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID <= 0 || got.ID != want.ID || got.Type != conversation.TypePersonal || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("personal conversation = %+v, want %+v", got, want)
	}
	var members, owners int
	var memberID int64
	if err := db.QueryRowContext(ctx, `
		SELECT count(*), min(user_id) FROM conversations_members WHERE conversation_id = $1`, got.ID).
		Scan(&members, &memberID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_personal WHERE user_id = $1", userID).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if members != 1 || memberID != userID || owners != 1 {
		t.Fatal("personal conversation must have exactly one owner and the same sole member")
	}
}

func TestConversationPersonalStoreCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	store := repository.NewConversationPersonalStore(db)
	repo := repository.NewConversationRepository(db)
	now := time.Date(2026, 11, 21, 12, 0, 0, 123456000, time.UTC)
	first, err := store.Create(ctx, ids[0], now)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || !first.CreatedAt.Equal(now) {
		t.Fatal("creation must return the stored timestamp")
	}
	assertConversationPersonal(t, ctx, db, first, ids[0])
	duplicate, err := store.Create(ctx, ids[0], now.Add(time.Hour))
	if duplicate != nil || !errors.Is(err, conversation.ErrAlreadyExists) {
		t.Fatalf("duplicate = (%+v, %v), want nil and ErrAlreadyExists", duplicate, err)
	}
	assertConversationPersonal(t, ctx, db, first, ids[0])
	assertConversationCounts(t, ctx, db, 1, 0, 1)
	second, err := store.Create(ctx, ids[1], now)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("different users must have different personal conversations")
	}
	assertConversationPersonal(t, ctx, db, second, ids[1])
	if got, err := repo.GetByIDForUser(ctx, first.ID, ids[1]); got != nil || !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("outsider lookup = (%+v, %v), want nil and ErrNotFound", got, err)
	}
	list, err := repo.ListByUser(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	assertConversationList(t, list, first)
	assertConversationCounts(t, ctx, db, 2, 0, 2)
}

func TestConversationPersonalStoreRollback(t *testing.T) {
	for _, scenario := range []string{"zero user", "negative user", "unknown user", "member failure", "canceled context"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			db, ids := conversationTestDB(t, ctx)
			userID := ids[0]
			callCtx := ctx
			var want error
			var code string
			switch scenario {
			case "zero user":
				userID, want = 0, user.ErrNotFound
			case "negative user":
				userID, want = -1, user.ErrNotFound
			case "unknown user":
				userID, code = ids[2]+1, "23503"
			case "member failure":
				if _, err := db.ExecContext(ctx, "ALTER TABLE conversations_members ADD CONSTRAINT test_reject_member CHECK (false)"); err != nil {
					t.Fatal(err)
				}
				code = "23514"
			case "canceled context":
				var stop context.CancelFunc
				callCtx, stop = context.WithCancel(ctx)
				stop()
				want = context.Canceled
			}
			got, err := repository.NewConversationPersonalStore(db).Create(callCtx, userID, time.Now())
			if got != nil || err == nil {
				t.Fatalf("result = (%+v, %v), want nil and an error", got, err)
			}
			if want != nil && !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			if code != "" {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != code {
					t.Fatalf("error = %v, want PostgreSQL code %s", err, code)
				}
			}
			assertConversationCounts(t, ctx, db, 0, 0, 0)
			var owners int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM conversations_personal").Scan(&owners); err != nil {
				t.Fatal(err)
			}
			if owners != 0 {
				t.Fatal("failed creation must leave no owner record")
			}
		})
	}
}

func TestConversationPersonalStoreConcurrentCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	store := repository.NewConversationPersonalStore(db)
	type result struct {
		conversation *conversation.Conversation
		err          error
	}
	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	results := make(chan result, 2)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	for i := range 2 {
		go func() {
			ready <- struct{}{}
			<-start
			c, err := store.Create(ctx, ids[0], now.Add(time.Duration(i)*time.Hour))
			results <- result{c, err}
		}()
	}
	for range 2 {
		<-ready
	}
	close(start)
	var winner *conversation.Conversation
	duplicates := 0
	for range 2 {
		got := <-results
		switch {
		case errors.Is(got.err, conversation.ErrAlreadyExists) && got.conversation == nil:
			duplicates++
		case got.err == nil && got.conversation != nil && winner == nil:
			winner = got.conversation
		default:
			t.Errorf("unexpected concurrent result: (%+v, %v)", got.conversation, got.err)
		}
	}
	if winner == nil || duplicates != 1 || t.Failed() {
		t.Fatal("expected one creation and one ErrAlreadyExists")
	}
	if !winner.CreatedAt.Equal(now) && !winner.CreatedAt.Equal(now.Add(time.Hour)) {
		t.Fatal("creation time must belong to one of the callers")
	}
	assertConversationPersonal(t, ctx, db, winner, ids[0])
	assertConversationCounts(t, ctx, db, 1, 0, 1)
}

func TestConversationRepositoryGetPersonalByUserErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	repo := repository.NewConversationRepository(db)
	got, err := repo.GetPersonalByUser(ctx, ids[0])
	if got != nil || !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("missing conversation = (%+v, %v), want nil and ErrNotFound", got, err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	got, err = repo.GetPersonalByUser(canceled, ids[0])
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lookup = (%+v, %v), want nil and context.Canceled", got, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetPersonalByUser(ctx, ids[0])
	if got != nil || err == nil || errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("closed database = (%+v, %v), want a storage error", got, err)
	}
}
