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
	"chat/internal/validation"
)

type fakeConversationUsers func(context.Context, string) (*user.User, error)

func (fake fakeConversationUsers) GetByUsername(ctx context.Context, username string) (*user.User, error) {
	return fake(ctx, username)
}

type fakeConversationRepository struct {
	getPersonalByUser func(context.Context, int64) (*conversation.Conversation, error)
	listByUser        func(context.Context, int64) ([]conversation.Conversation, error)
	getDirectByUsers  func(context.Context, int64, int64) (*conversation.Conversation, error)
	getByIDForUser    func(context.Context, int64, int64) (*conversation.Conversation, error)
}

func (fake fakeConversationRepository) GetPersonalByUser(ctx context.Context, userID int64) (*conversation.Conversation, error) {
	return fake.getPersonalByUser(ctx, userID)
}

func (fake fakeConversationRepository) ListByUser(ctx context.Context, currentUserID int64) ([]conversation.Conversation, error) {
	return fake.listByUser(ctx, currentUserID)
}

func (fake fakeConversationRepository) GetDirectByUsers(ctx context.Context, firstUserID, secondUserID int64) (*conversation.Conversation, error) {
	return fake.getDirectByUsers(ctx, firstUserID, secondUserID)
}

func (fake fakeConversationRepository) GetByIDForUser(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error) {
	return fake.getByIDForUser(ctx, conversationID, currentUserID)
}

type fakeConversationDirectStore func(context.Context, int64, int64, time.Time) (*conversation.Conversation, error)

func (fake fakeConversationDirectStore) Create(ctx context.Context, firstUserID, secondUserID int64, createdAt time.Time) (*conversation.Conversation, error) {
	return fake(ctx, firstUserID, secondUserID, createdAt)
}

var (
	_ service.ConversationUsers       = fakeConversationUsers(nil)
	_ service.ConversationUsers       = (*repository.UserRepository)(nil)
	_ service.ConversationRepository  = fakeConversationRepository{}
	_ service.ConversationRepository  = (*repository.ConversationRepository)(nil)
	_ service.ConversationDirectStore = fakeConversationDirectStore(nil)
	_ service.ConversationDirectStore = (*repository.ConversationDirectStore)(nil)
)

func TestConversationServiceCreateDirect(t *testing.T) {
	for _, tt := range []struct {
		name     string
		username string
		existing bool
	}{
		{"new conversation", "anna", false},
		{"normalized username", " \tANNA\n", false},
		{"Unicode whitespace", "\u00a0Anna\u2003", false},
		{"existing conversation", "anna", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			before := time.Now().UTC()
			want := &conversation.Conversation{ID: 7, Type: conversation.TypeDirect, CreatedAt: before.Add(-time.Hour)}
			var calls []string
			users := fakeConversationUsers(func(gotCtx context.Context, username string) (*user.User, error) {
				if gotCtx != ctx || username != "anna" {
					t.Fatal("user lookup must receive the caller's context and normalized username")
				}
				calls = append(calls, "user")
				return &user.User{ID: 20, Username: username}, nil
			})
			store := fakeConversationDirectStore(func(gotCtx context.Context, firstID, secondID int64, createdAt time.Time) (*conversation.Conversation, error) {
				if gotCtx != ctx || firstID != 10 || secondID != 20 {
					t.Fatal("creation must receive both user IDs and the caller's context")
				}
				if createdAt.Location() != time.UTC || createdAt.Before(before) || createdAt.After(time.Now().UTC()) {
					t.Fatal("service must supply the current time in UTC")
				}
				calls = append(calls, "create")
				if tt.existing {
					return nil, fmt.Errorf("pair conflict: %w", conversation.ErrAlreadyExists)
				}
				want.CreatedAt = createdAt
				return want, nil
			})
			repo := fakeConversationRepository{getDirectByUsers: func(gotCtx context.Context, firstID, secondID int64) (*conversation.Conversation, error) {
				if !tt.existing || gotCtx != ctx || firstID != 10 || secondID != 20 {
					t.Fatal("lookup must only follow a pair conflict and use the same users and context")
				}
				calls = append(calls, "lookup")
				return want, nil
			}}
			got, err := service.NewConversationService(users, repo, store, nil).CreateDirect(ctx, 10, tt.username)
			if err != nil || got != want {
				t.Fatalf("CreateDirect = (%+v, %v), want the stored conversation", got, err)
			}
			wantCalls := "user,create"
			if tt.existing {
				wantCalls += ",lookup"
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

func TestConversationServiceCreateDirectInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		name     string
		userID   int64
		username string
		want     error
	}{
		{"zero user ID", 0, "anna", conversation.ErrInvalidParticipants},
		{"negative user ID", -1, "anna", conversation.ErrInvalidParticipants},
		{"empty username", 10, " \t", validation.ErrInvalidUsername},
		{"short username", 10, "ab", validation.ErrInvalidUsername},
		{"long username", 10, strings.Repeat("a", 33), validation.ErrInvalidUsername},
		{"invalid character", 10, "an-na", validation.ErrInvalidUsername},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// No dependency may be called for invalid input.
			s := service.NewConversationService(nil, nil, nil, nil)
			got, err := s.CreateDirect(t.Context(), tt.userID, tt.username)
			if got != nil || !errors.Is(err, tt.want) {
				t.Fatalf("CreateDirect = (%+v, %v), want nil and %v", got, err, tt.want)
			}
		})
	}
}

func TestConversationServiceCreateDirectFailures(t *testing.T) {
	dbError := errors.New("database unavailable")
	for _, tt := range []struct {
		name       string
		otherID    int64
		userError  error
		storeError error
		readError  error
		cancelAt   string
		want       error
		wantCalls  string
	}{
		{"missing user", 20, user.ErrNotFound, nil, nil, "", user.ErrNotFound, "user"},
		{"user lookup failure", 20, dbError, nil, nil, "", dbError, "user"},
		{"self dialogue", 10, nil, nil, nil, "", conversation.ErrInvalidParticipants, "user"},
		{"creation failure", 20, nil, dbError, nil, "", dbError, "user,create"},
		{"existing lookup failure", 20, nil, conversation.ErrAlreadyExists, dbError, "", dbError, "user,create,lookup"},
		{"missing pair after conflict", 20, nil, conversation.ErrAlreadyExists, conversation.ErrNotFound, "", conversation.ErrNotFound, "user,create,lookup"},
		{"canceled before lookup", 20, nil, nil, nil, "before", context.Canceled, ""},
		{"canceled after user lookup", 20, nil, nil, nil, "user", context.Canceled, "user"},
		{"canceled after conflict", 20, nil, conversation.ErrAlreadyExists, nil, "create", context.Canceled, "user,create"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelAt == "before" {
				cancel()
			}
			var calls []string
			users := fakeConversationUsers(func(gotCtx context.Context, username string) (*user.User, error) {
				calls = append(calls, "user")
				if tt.cancelAt == "user" {
					cancel()
				}
				if tt.userError != nil {
					return nil, tt.userError
				}
				return &user.User{ID: tt.otherID}, nil
			})
			store := fakeConversationDirectStore(func(context.Context, int64, int64, time.Time) (*conversation.Conversation, error) {
				calls = append(calls, "create")
				if tt.cancelAt == "create" {
					cancel()
				}
				return nil, tt.storeError
			})
			repo := fakeConversationRepository{getDirectByUsers: func(context.Context, int64, int64) (*conversation.Conversation, error) {
				calls = append(calls, "lookup")
				return nil, tt.readError
			}}
			got, err := service.NewConversationService(users, repo, store, nil).CreateDirect(ctx, 10, " ANNA ")
			if got != nil || !errors.Is(err, tt.want) {
				t.Fatalf("CreateDirect = (%+v, %v), want nil and %v", got, err, tt.want)
			}
			if strings.Join(calls, ",") != tt.wantCalls {
				t.Fatalf("calls = %v, want %s", calls, tt.wantCalls)
			}
		})
	}
}
