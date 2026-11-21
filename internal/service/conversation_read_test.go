package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/service"
)

func TestConversationServiceGetByID(t *testing.T) {
	ctx := t.Context()
	want := &conversation.Conversation{ID: 7, Type: conversation.TypeDirect, CreatedAt: time.Now()}
	calls := 0
	repo := fakeConversationRepository{getByIDForUser: func(gotCtx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error) {
		calls++
		if gotCtx != ctx || conversationID != 7 || currentUserID != 42 {
			t.Fatal("lookup must receive the caller's context, conversation ID and current user ID")
		}
		return want, nil
	}}
	got, err := service.NewConversationService(nil, repo, nil, nil).GetByID(ctx, 7, 42)
	if err != nil || got != want || calls != 1 {
		t.Fatalf("result = (%+v, %v), calls = %d; want original conversation and one lookup", got, err, calls)
	}
}

func TestConversationServiceGetByIDRejectsInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		name           string
		conversationID int64
		userID         int64
		canceled       bool
		want           error
	}{
		{"zero conversation", 0, 42, false, conversation.ErrNotFound},
		{"negative conversation", -1, 42, false, conversation.ErrNotFound},
		{"zero user", 7, 0, false, conversation.ErrNotFound},
		{"negative user", 7, -1, false, conversation.ErrNotFound},
		{"canceled context", 7, 42, true, context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.canceled {
				cancel()
			}
			got, err := service.NewConversationService(nil, nil, nil, nil).GetByID(ctx, tt.conversationID, tt.userID)
			if got != nil || !errors.Is(err, tt.want) {
				t.Fatalf("result = (%+v, %v), want nil and %v", got, err, tt.want)
			}
		})
	}
}

func TestConversationServiceGetByIDRepositoryErrors(t *testing.T) {
	for _, want := range []error{conversation.ErrNotFound, context.Canceled, errors.New("database unavailable")} {
		t.Run(want.Error(), func(t *testing.T) {
			repo := fakeConversationRepository{getByIDForUser: func(context.Context, int64, int64) (*conversation.Conversation, error) {
				return nil, fmt.Errorf("lookup failed: %w", want)
			}}
			got, err := service.NewConversationService(nil, repo, nil, nil).GetByID(t.Context(), 7, 42)
			if got != nil || !errors.Is(err, want) {
				t.Fatalf("result = (%+v, %v), want nil and %v", got, err, want)
			}
		})
	}
}
