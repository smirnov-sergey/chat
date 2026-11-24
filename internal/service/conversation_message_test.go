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

type fakeConversationMessageRepository struct {
	list func(context.Context, int64, *int64) (*conversation.MessagePage, error)
}

func (fake fakeConversationMessageRepository) List(ctx context.Context, conversationID int64, beforeID *int64) (*conversation.MessagePage, error) {
	return fake.list(ctx, conversationID, beforeID)
}

type fakeConversationMessageStore func(context.Context, int64, int64, string) (*conversation.Message, error)

func (fake fakeConversationMessageStore) Create(ctx context.Context, conversationID, userID int64, text string) (*conversation.Message, error) {
	return fake(ctx, conversationID, userID, text)
}

var (
	_ service.ConversationMessageAccess     = (*repository.ConversationRepository)(nil)
	_ service.ConversationMessageRepository = (*repository.ConversationMessageRepository)(nil)
	_ service.ConversationMessageRepository = fakeConversationMessageRepository{}
	_ service.ConversationMessageStore      = (*repository.ConversationMessageStore)(nil)
	_ service.ConversationMessageStore      = fakeConversationMessageStore(nil)
)

func TestConversationMessageServiceCreate(t *testing.T) {
	ctx := t.Context()
	text := "  Привет 👋\n\tмир  "
	want := &conversation.Message{ID: 19, ConversationID: 7, SenderUserID: 42, Text: text, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	calls := 0
	store := fakeConversationMessageStore(func(gotCtx context.Context, id, userID int64, gotText string) (*conversation.Message, error) {
		calls++
		if gotCtx != ctx || id != 7 || userID != 42 || gotText != text {
			t.Fatal("store must receive original context, IDs and unchanged text")
		}
		return want, nil
	})
	got, err := service.NewConversationMessageService(nil, nil, store).Create(ctx, 7, 42, text)
	if err != nil || got != want || calls != 1 {
		t.Fatalf("result = (%+v, %v), want stored message", got, err)
	}
}

func TestConversationMessageServiceCreateInvalidInput(t *testing.T) {
	for _, ids := range [][2]int64{{0, 42}, {-1, 42}, {7, 0}, {7, -1}} {
		got, err := service.NewConversationMessageService(nil, nil, nil).Create(t.Context(), ids[0], ids[1], "text")
		if got != nil || !errors.Is(err, conversation.ErrNotFound) {
			t.Fatalf("IDs %v: result = (%+v, %v), want not found", ids, got, err)
		}
	}
	got, err := service.NewConversationMessageService(nil, nil, nil).Create(t.Context(), 7, 42, " \t\n")
	if got != nil || !errors.Is(err, validation.ErrInvalidConversationMessageText) {
		t.Fatalf("result = (%+v, %v), want invalid text", got, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err = service.NewConversationMessageService(nil, nil, nil).Create(ctx, 7, 42, "text")
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("result = (%+v, %v), want canceled", got, err)
	}
}

func TestConversationMessageServiceCreateFailures(t *testing.T) {
	for _, want := range []error{conversation.ErrNotFound, context.Canceled, errors.New("database unavailable")} {
		store := fakeConversationMessageStore(func(context.Context, int64, int64, string) (*conversation.Message, error) {
			return &conversation.Message{ID: 19}, fmt.Errorf("store: %w", want)
		})
		got, err := service.NewConversationMessageService(nil, nil, store).Create(t.Context(), 7, 42, "text")
		if got != nil || !errors.Is(err, want) {
			t.Fatalf("result = (%+v, %v), want error %v", got, err, want)
		}
	}
}
