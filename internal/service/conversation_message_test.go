package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/repository"
	"chat/internal/service"
	"chat/internal/validation"
)

type fakeConversationMessageRepository func(context.Context, *conversation.Message) error

func (fake fakeConversationMessageRepository) Create(ctx context.Context, message *conversation.Message) error {
	return fake(ctx, message)
}

var (
	_ service.ConversationMessageAccess     = (*repository.ConversationRepository)(nil)
	_ service.ConversationMessageRepository = (*repository.ConversationMessageRepository)(nil)
	_ service.ConversationMessageRepository = fakeConversationMessageRepository(nil)
)

func TestConversationMessageServiceCreate(t *testing.T) {
	for _, kind := range []conversation.Type{conversation.TypeDirect, conversation.TypePersonal} {
		t.Run(string(kind), func(t *testing.T) {
			ctx := t.Context()
			text := "  Привет 👋\n\tмир  "
			before := time.Now().UTC()
			storedAt := before.Truncate(time.Microsecond)
			checked := false
			access := fakeConversationRepository{getByIDForUser: func(gotCtx context.Context, id, userID int64) (*conversation.Conversation, error) {
				if gotCtx != ctx || id != 7 || userID != 42 {
					t.Fatal("access check must receive the original context, conversation and sender")
				}
				checked = true
				return &conversation.Conversation{ID: id, Type: kind}, nil
			}}
			var saved *conversation.Message
			repo := fakeConversationMessageRepository(func(gotCtx context.Context, message *conversation.Message) error {
				if !checked || gotCtx != ctx || message.ID != 0 || message.ConversationID != 7 || message.SenderUserID != 42 || message.Text != text {
					t.Fatal("creation must follow access checking and preserve the text and IDs")
				}
				if message.CreatedAt.Location() != time.UTC || message.CreatedAt.Before(before) || message.CreatedAt.After(time.Now().UTC()) {
					t.Fatal("service must supply current time in UTC")
				}
				message.ID = 19
				message.CreatedAt = storedAt
				saved = message
				return nil
			})
			got, err := service.NewConversationMessageService(access, repo).Create(ctx, 7, 42, text)
			if err != nil || got == nil || got != saved || got.ID != 19 || !got.CreatedAt.Equal(storedAt) {
				t.Fatalf("result = (%+v, %v), want repository result", got, err)
			}
		})
	}
}

func TestConversationMessageServiceCreateInvalidIDs(t *testing.T) {
	for _, ids := range [][2]int64{{0, 42}, {-1, 42}, {7, 0}, {7, -1}} {
		got, err := service.NewConversationMessageService(nil, nil).Create(t.Context(), ids[0], ids[1], "text")
		if got != nil || !errors.Is(err, conversation.ErrNotFound) {
			t.Fatalf("IDs %v: result = (%+v, %v), want not found", ids, got, err)
		}
	}
}

func TestConversationMessageServiceCreateFailures(t *testing.T) {
	dbError := errors.New("database unavailable")
	for _, tt := range []struct {
		name              string
		text              string
		lookupError       error
		createError       error
		cancelBefore      bool
		cancelAfterLookup bool
		want              error
		wantLookup        bool
		wantCreate        bool
	}{
		{"not found before text validation", "", fmt.Errorf("lookup: %w", conversation.ErrNotFound), nil, false, false, conversation.ErrNotFound, true, false},
		{"lookup failure", "text", dbError, nil, false, false, dbError, true, false},
		{"invalid text", " \t\n", nil, nil, false, false, validation.ErrInvalidConversationMessageText, true, false},
		{"create failure", "text", nil, dbError, false, false, dbError, true, true},
		{"create cancellation", "text", nil, context.Canceled, false, false, context.Canceled, true, true},
		{"canceled before lookup", "text", nil, nil, true, false, context.Canceled, false, false},
		{"canceled after lookup", "text", nil, nil, false, true, context.Canceled, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelBefore {
				cancel()
			}
			lookedUp, created := false, false
			access := fakeConversationRepository{getByIDForUser: func(context.Context, int64, int64) (*conversation.Conversation, error) {
				lookedUp = true
				if tt.cancelAfterLookup {
					cancel()
				}
				return &conversation.Conversation{ID: 7}, tt.lookupError
			}}
			repo := fakeConversationMessageRepository(func(_ context.Context, message *conversation.Message) error {
				created = true
				message.ID = 19
				return tt.createError
			})
			got, err := service.NewConversationMessageService(access, repo).Create(ctx, 7, 42, tt.text)
			if got != nil || !errors.Is(err, tt.want) || lookedUp != tt.wantLookup || created != tt.wantCreate {
				t.Fatalf("result = (%+v, %v), lookup/create = %v/%v; want error %v, calls %v/%v", got, err, lookedUp, created, tt.want, tt.wantLookup, tt.wantCreate)
			}
		})
	}
}
