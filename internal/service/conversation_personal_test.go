package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/model/user"
	"chat/internal/repository"
	"chat/internal/service"
)

type fakeConversationPersonalStore func(context.Context, int64, time.Time) (*conversation.Conversation, error)

func (fake fakeConversationPersonalStore) Create(ctx context.Context, userID int64, createdAt time.Time) (*conversation.Conversation, error) {
	return fake(ctx, userID, createdAt)
}

var (
	_ service.ConversationPersonalStore = fakeConversationPersonalStore(nil)
	_ service.ConversationPersonalStore = (*repository.ConversationPersonalStore)(nil)
)

func TestConversationServiceCreatePersonal(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing_%v", existing), func(t *testing.T) {
			ctx := t.Context()
			before := time.Now().UTC()
			want := &conversation.Conversation{ID: 7, Type: conversation.TypePersonal, CreatedAt: before.Add(-time.Hour)}
			var calls []string
			store := fakeConversationPersonalStore(func(gotCtx context.Context, userID int64, createdAt time.Time) (*conversation.Conversation, error) {
				calls = append(calls, "create")
				if gotCtx != ctx || userID != 42 {
					t.Fatal("store must receive the caller's context and current user ID")
				}
				if createdAt.Location() != time.UTC || createdAt.Before(before) || createdAt.After(time.Now().UTC()) {
					t.Fatal("service must supply the current time in UTC")
				}
				if existing {
					return nil, fmt.Errorf("owner conflict: %w", conversation.ErrAlreadyExists)
				}
				want.CreatedAt = createdAt
				return want, nil
			})
			repo := fakeConversationRepository{getPersonalByUser: func(gotCtx context.Context, userID int64) (*conversation.Conversation, error) {
				if !existing || gotCtx != ctx || userID != 42 {
					t.Fatal("lookup must follow a conflict and use the same owner and context")
				}
				calls = append(calls, "lookup")
				return want, nil
			}}
			got, err := service.NewConversationService(nil, repo, nil, store).CreatePersonal(ctx, 42)
			if err != nil || got != want {
				t.Fatalf("result = (%+v, %v), want the stored personal conversation", got, err)
			}
			wantCalls := "create"
			if existing {
				wantCalls = "create,lookup"
				if !got.CreatedAt.Equal(before.Add(-time.Hour)) {
					t.Fatal("existing creation time must remain unchanged")
				}
			}
			if strings.Join(calls, ",") != wantCalls {
				t.Fatalf("calls = %v, want %s", calls, wantCalls)
			}
		})
	}
}

func TestConversationServiceCreatePersonalInvalidInput(t *testing.T) {
	for _, userID := range []int64{0, -1} {
		got, err := service.NewConversationService(nil, nil, nil, nil).CreatePersonal(t.Context(), userID)
		if got != nil || !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("user %d: result = (%+v, %v), want nil and user.ErrNotFound", userID, got, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := service.NewConversationService(nil, nil, nil, nil).CreatePersonal(ctx, 42)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result = (%+v, %v), want nil and context.Canceled", got, err)
	}
}

func TestConversationServiceCreatePersonalFailures(t *testing.T) {
	dbError := errors.New("database unavailable")
	for _, tt := range []struct {
		name              string
		storeError        error
		lookupError       error
		cancelAfterCreate bool
		want              error
		wantLookup        bool
	}{
		{"store error", dbError, nil, false, dbError, false},
		{"store cancellation", context.Canceled, nil, false, context.Canceled, false},
		{"lookup error", conversation.ErrAlreadyExists, dbError, false, dbError, true},
		{"missing after conflict", conversation.ErrAlreadyExists, conversation.ErrNotFound, false, conversation.ErrNotFound, true},
		{"canceled after conflict", conversation.ErrAlreadyExists, nil, true, context.Canceled, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			storeCalls, lookupCalls := 0, 0
			store := fakeConversationPersonalStore(func(context.Context, int64, time.Time) (*conversation.Conversation, error) {
				storeCalls++
				if tt.cancelAfterCreate {
					cancel()
				}
				return nil, fmt.Errorf("create failed: %w", tt.storeError)
			})
			repo := fakeConversationRepository{getPersonalByUser: func(context.Context, int64) (*conversation.Conversation, error) {
				lookupCalls++
				return nil, tt.lookupError
			}}
			got, err := service.NewConversationService(nil, repo, nil, store).CreatePersonal(ctx, 42)
			if got != nil || !errors.Is(err, tt.want) {
				t.Fatalf("result = (%+v, %v), want nil and %v", got, err, tt.want)
			}
			wantLookupCalls := 0
			if tt.wantLookup {
				wantLookupCalls = 1
			}
			if storeCalls != 1 || lookupCalls != wantLookupCalls {
				t.Fatalf("store calls=%d, lookup calls=%d; want 1 and %d", storeCalls, lookupCalls, wantLookupCalls)
			}
		})
	}
}
