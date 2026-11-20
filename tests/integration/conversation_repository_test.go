//go:build integration

package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"chat/internal/migrate"
	"chat/internal/model/conversation"
	"chat/internal/repository"

	"github.com/jackc/pgx/v5/pgconn"
)

func conversationTestDB(t *testing.T, ctx context.Context) (*sql.DB, [3]int64) {
	t.Helper()
	db := integrationDB(t)
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	files := os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))
	if _, err := migrate.Up(ctx, db, files); err != nil {
		t.Fatal(err)
	}
	var ids [3]int64
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for i, username := range []string{"ivan", "anna", "oleg"} {
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (username, name, created_at, updated_at)
			VALUES ($1, $1, $2, $2) RETURNING id`, username, now).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	return db, ids
}

func assertConversationCounts(t *testing.T, ctx context.Context, db *sql.DB, conversations, pairs, members int) {
	t.Helper()
	var gotConversations, gotPairs, gotMembers int
	if err := db.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM conversations),
		       (SELECT count(*) FROM conversations_direct),
		       (SELECT count(*) FROM conversations_members)`).
		Scan(&gotConversations, &gotPairs, &gotMembers); err != nil {
		t.Fatal(err)
	}
	if gotConversations != conversations || gotPairs != pairs || gotMembers != members {
		t.Fatalf("conversations/pairs/members = %d/%d/%d, want %d/%d/%d",
			gotConversations, gotPairs, gotMembers, conversations, pairs, members)
	}
}

func assertConversationDirect(t *testing.T, ctx context.Context, db *sql.DB, got *conversation.Conversation, firstID, secondID int64) {
	t.Helper()
	if got == nil || got.ID <= 0 || got.Type != conversation.TypeDirect {
		t.Fatalf("unexpected conversation: %+v", got)
	}
	var lowUserID, highUserID int64
	var storedType string
	var createdAt time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT d.low_user_id, d.high_user_id, c.type, c.created_at
		FROM conversations_direct d JOIN conversations c ON c.id = d.conversation_id
		WHERE c.id = $1`, got.ID).Scan(&lowUserID, &highUserID, &storedType, &createdAt); err != nil {
		t.Fatal(err)
	}
	if lowUserID != min(firstID, secondID) || highUserID != max(firstID, secondID) ||
		storedType != string(got.Type) || !createdAt.Equal(got.CreatedAt) {
		t.Fatal("stored conversation and canonical pair must match the result")
	}
	rows, err := db.QueryContext(ctx, `
		SELECT user_id FROM conversations_members WHERE conversation_id = $1 ORDER BY user_id`, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := []int64{lowUserID, highUserID}
	i := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if i >= len(want) || id != want[i] {
			t.Fatalf("unexpected member %d at position %d", id, i)
		}
		i++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if i != len(want) {
		t.Fatalf("members = %d, want 2", i)
	}
}

func TestConversationDirectStoreCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	store := repository.NewConversationDirectStore(db)
	now := time.Date(2026, 11, 20, 15, 0, 0, 123456000, time.FixedZone("UTC+3", 3*60*60))
	first, err := store.Create(ctx, ids[1], ids[0], now)
	if err != nil {
		t.Fatal(err)
	}
	assertConversationDirect(t, ctx, db, first, ids[0], ids[1])
	if !first.CreatedAt.Equal(now) {
		t.Fatal("creation time must preserve the supplied instant")
	}
	for _, pair := range [][2]int64{{ids[0], ids[1]}, {ids[1], ids[0]}} {
		got, err := store.Create(ctx, pair[0], pair[1], now.Add(time.Hour))
		if got != nil || !errors.Is(err, conversation.ErrAlreadyExists) {
			t.Fatalf("duplicate result = (%+v, %v), want nil and ErrAlreadyExists", got, err)
		}
		got, err = repository.NewConversationRepository(db).GetDirectByUsers(ctx, pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != first.ID || !got.CreatedAt.Equal(first.CreatedAt) {
			t.Fatal("repeated calls must preserve conversation ID and creation time")
		}
		assertConversationDirect(t, ctx, db, got, ids[0], ids[1])
		assertConversationCounts(t, ctx, db, 1, 1, 2)
	}
	other, err := store.Create(ctx, ids[0], ids[2], now)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == first.ID {
		t.Fatal("different pairs must have different conversations")
	}
	assertConversationDirect(t, ctx, db, other, ids[0], ids[2])
	assertConversationCounts(t, ctx, db, 2, 2, 4)
}

func TestConversationDirectStoreInvalidParticipants(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	store := repository.NewConversationDirectStore(db)
	for _, pair := range [][2]int64{{ids[0], ids[0]}, {0, ids[0]}, {ids[0], 0}, {-1, ids[0]}, {ids[0], -1}} {
		got, err := store.Create(ctx, pair[0], pair[1], time.Now())
		if got != nil || !errors.Is(err, conversation.ErrInvalidParticipants) {
			t.Fatalf("pair %v: result = (%+v, %v), want nil and ErrInvalidParticipants", pair, got, err)
		}
	}
	assertConversationCounts(t, ctx, db, 0, 0, 0)
}

func TestConversationDirectStoreRollback(t *testing.T) {
	for _, scenario := range []string{"unknown user", "member insert failure", "canceled context"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			db, ids := conversationTestDB(t, ctx)
			store := repository.NewConversationDirectStore(db)
			callCtx := ctx
			otherID := ids[1]
			var wantCode string
			switch scenario {
			case "unknown user":
				otherID = ids[2] + 1
				wantCode = "23503"
			case "member insert failure":
				// Fail the final write after the conversation and pair were inserted.
				query := fmt.Sprintf("ALTER TABLE conversations_members ADD CONSTRAINT test_reject_member CHECK (user_id <> %d)", otherID)
				if _, err := db.ExecContext(ctx, query); err != nil {
					t.Fatal(err)
				}
				wantCode = "23514"
			case "canceled context":
				var stop context.CancelFunc
				callCtx, stop = context.WithCancel(ctx)
				stop()
			}
			got, err := store.Create(callCtx, ids[0], otherID, time.Now())
			if got != nil || err == nil {
				t.Fatalf("result = (%+v, %v), want nil and an error", got, err)
			}
			if wantCode != "" {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != wantCode {
					t.Fatalf("error = %v, want PostgreSQL code %s", err, wantCode)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			assertConversationCounts(t, ctx, db, 0, 0, 0)
		})
	}
}

func TestConversationDirectStoreConcurrentCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	store := repository.NewConversationDirectStore(db)
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	type result struct {
		conversation *conversation.Conversation
		err          error
	}
	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	results := make(chan result, 2)
	for i, pair := range [][2]int64{{ids[0], ids[1]}, {ids[1], ids[0]}} {
		go func() {
			ready <- struct{}{}
			<-start
			got, err := store.Create(ctx, pair[0], pair[1], now.Add(time.Duration(i)*time.Hour))
			results <- result{got, err}
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
		if errors.Is(got.err, conversation.ErrAlreadyExists) {
			duplicates++
			if got.conversation != nil {
				t.Error("duplicate creation must return no conversation")
			}
			continue
		}
		if got.err != nil || got.conversation == nil {
			t.Errorf("concurrent result = (%+v, %v), want a conversation", got.conversation, got.err)
			continue
		}
		if winner == nil {
			winner = got.conversation
		} else {
			t.Error("only one creation may succeed")
		}
	}
	if winner == nil || duplicates != 1 {
		t.Error("expected one creation and one ErrAlreadyExists")
	}
	if t.Failed() {
		t.FailNow()
	}
	if !winner.CreatedAt.Equal(now) && !winner.CreatedAt.Equal(now.Add(time.Hour)) {
		t.Fatal("creation time must belong to one of the callers")
	}
	assertConversationDirect(t, ctx, db, winner, ids[0], ids[1])
	existing, err := repository.NewConversationRepository(db).GetDirectByUsers(ctx, ids[1], ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if existing.ID != winner.ID || !existing.CreatedAt.Equal(winner.CreatedAt) {
		t.Fatal("lookup after conflict must return the winning conversation")
	}
	assertConversationCounts(t, ctx, db, 1, 1, 2)
}

func TestConversationRepositoryGetDirectByUsers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	repo := repository.NewConversationRepository(db)

	got, err := repo.GetDirectByUsers(ctx, ids[0], ids[1])
	if got != nil || !errors.Is(err, conversation.ErrNotFound) {
		t.Fatalf("missing pair = (%+v, %v), want nil and ErrNotFound", got, err)
	}

	canceled, stop := context.WithCancel(ctx)
	stop()
	got, err = repo.GetDirectByUsers(canceled, ids[0], ids[1])
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lookup = (%+v, %v), want nil and context.Canceled", got, err)
	}
	assertConversationCounts(t, ctx, db, 0, 0, 0)
}

func TestConversationRepositoryCreate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, _ := conversationTestDB(t, ctx)
	repo := repository.NewConversationRepository(db)
	now := time.Date(2026, 11, 20, 12, 0, 0, 123456000, time.UTC)
	c := conversation.Conversation{Type: conversation.TypeDirect, CreatedAt: now}
	if err := repo.Create(ctx, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID <= 0 || c.Type != conversation.TypeDirect || !c.CreatedAt.Equal(now) {
		t.Fatalf("unexpected created conversation: %+v", c)
	}
	assertConversationCounts(t, ctx, db, 1, 0, 0)

	invalid := conversation.Conversation{Type: "unknown", CreatedAt: now}
	before := invalid
	err := repo.Create(ctx, &invalid)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("invalid type error = %v, want a CHECK violation", err)
	}
	if invalid != before {
		t.Fatal("failed creation must not modify the supplied model")
	}

	canceled, stop := context.WithCancel(ctx)
	stop()
	pending := conversation.Conversation{Type: conversation.TypeDirect, CreatedAt: now}
	before = pending
	if err := repo.Create(canceled, &pending); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled creation error = %v, want context.Canceled", err)
	}
	if pending != before {
		t.Fatal("canceled creation must not modify the supplied model")
	}
	assertConversationCounts(t, ctx, db, 1, 0, 0)
}
