package service_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/model/user"
	"chat/internal/service"
)

func TestConversationServiceListByUser(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name string
		want []conversation.Conversation
	}{
		{"ordered conversations", []conversation.Conversation{
			{ID: 7, Type: conversation.TypeDirect, CreatedAt: now},
			{ID: 3, Type: conversation.TypeDirect, CreatedAt: now.Add(-time.Hour)},
		}},
		{"no conversations", []conversation.Conversation{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			calls := 0
			original := slices.Clone(tt.want)
			repo := fakeConversationRepository{listByUser: func(gotCtx context.Context, currentUserID int64) ([]conversation.Conversation, error) {
				calls++
				if gotCtx != ctx || currentUserID != 42 {
					t.Fatal("repository must receive the caller's context and current user ID")
				}
				return tt.want, nil
			}}
			got, err := service.NewConversationService(nil, repo, nil, nil, nil).ListByUser(ctx, 42)
			if err != nil || calls != 1 || got == nil || !slices.Equal(got, original) {
				t.Fatalf("result = (%+v, %v), calls = %d; want unchanged list and one lookup", got, err, calls)
			}
		})
	}
}

func TestConversationServiceListByUserInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		name     string
		userID   int64
		canceled bool
		want     error
	}{
		{"zero user ID", 0, false, user.ErrNotFound},
		{"negative user ID", -1, false, user.ErrNotFound},
		{"canceled context", 42, true, context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.canceled {
				cancel()
			}
			got, err := service.NewConversationService(nil, nil, nil, nil, nil).ListByUser(ctx, tt.userID)
			if got != nil || !errors.Is(err, tt.want) {
				t.Fatalf("result = (%+v, %v), want nil and %v", got, err, tt.want)
			}
		})
	}
}

func TestConversationServiceListByUserRepositoryErrors(t *testing.T) {
	for _, want := range []error{errors.New("database unavailable"), context.Canceled, context.DeadlineExceeded} {
		t.Run(want.Error(), func(t *testing.T) {
			repo := fakeConversationRepository{listByUser: func(context.Context, int64) ([]conversation.Conversation, error) {
				return nil, fmt.Errorf("query failed: %w", want)
			}}
			got, err := service.NewConversationService(nil, repo, nil, nil, nil).ListByUser(t.Context(), 42)
			if got != nil || !errors.Is(err, want) {
				t.Fatalf("result = (%+v, %v), want nil and %v", got, err, want)
			}
		})
	}
}
