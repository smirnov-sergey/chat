package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"chat/internal/model/conversation"
	"chat/internal/service"
)

func TestConversationMessageServiceList(t *testing.T) {
	cursor := int64(150)
	nextCursor := int64(100)
	for _, kind := range []conversation.Type{conversation.TypeDirect, conversation.TypePersonal} {
		for _, beforeID := range []*int64{nil, &cursor} {
			for _, empty := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/cursor_%v/empty_%v", kind, beforeID != nil, empty), func(t *testing.T) {
					ctx := t.Context()
					want := &conversation.MessagePage{Messages: []conversation.Message{}}
					if !empty {
						want.Messages = []conversation.Message{{ID: 100, ConversationID: 7, SenderUserID: 42, Text: "hello"}}
						want.NextCursor = &nextCursor
					}
					checked, listed := false, false
					access := fakeConversationRepository{getByIDForUser: func(gotCtx context.Context, id, userID int64) (*conversation.Conversation, error) {
						if gotCtx != ctx || id != 7 || userID != 42 {
							t.Fatal("incorrect access check arguments")
						}
						checked = true
						return &conversation.Conversation{ID: id, Type: kind}, nil
					}}
					repo := fakeConversationMessageRepository{list: func(gotCtx context.Context, id int64, gotBeforeID *int64) (*conversation.MessagePage, error) {
						if !checked || gotCtx != ctx || id != 7 || gotBeforeID != beforeID {
							t.Fatal("listing must follow access checking with the original context and cursor")
						}
						listed = true
						return want, nil
					}}
					got, err := service.NewConversationMessageService(access, repo, nil).List(ctx, 7, 42, beforeID)
					if err != nil || got != want || !listed || cursor != 150 {
						t.Fatalf("result = (%+v, %v), want unchanged repository page", got, err)
					}
				})
			}
		}
	}
}

func TestConversationMessageServiceListInvalidIDs(t *testing.T) {
	for _, ids := range [][2]int64{{0, 42}, {-1, 42}, {7, 0}, {7, -1}} {
		got, err := service.NewConversationMessageService(nil, nil, nil).List(t.Context(), ids[0], ids[1], nil)
		if got != nil || !errors.Is(err, conversation.ErrNotFound) {
			t.Fatalf("IDs %v: result = (%+v, %v), want not found", ids, got, err)
		}
	}
}

func TestConversationMessageServiceListFailures(t *testing.T) {
	dbError := errors.New("database unavailable")
	zero, negative := int64(0), int64(-1)
	for _, tt := range []struct {
		name                            string
		beforeID                        *int64
		accessError, listError          error
		cancelBefore, cancelAfterAccess bool
		want                            error
		wantAccess, wantList            bool
	}{
		{"not found before cursor validation", &zero, fmt.Errorf("lookup: %w", conversation.ErrNotFound), nil, false, false, conversation.ErrNotFound, true, false},
		{"access failure", nil, dbError, nil, false, false, dbError, true, false},
		{"zero cursor", &zero, nil, nil, false, false, conversation.ErrInvalidCursor, true, false},
		{"negative cursor", &negative, nil, nil, false, false, conversation.ErrInvalidCursor, true, false},
		{"list failure", nil, nil, dbError, false, false, dbError, true, true},
		{"list cancellation", nil, nil, context.Canceled, false, false, context.Canceled, true, true},
		{"canceled before access", nil, nil, nil, true, false, context.Canceled, false, false},
		{"canceled after access", nil, nil, nil, false, true, context.Canceled, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelBefore {
				cancel()
			}
			checked, listed := false, false
			access := fakeConversationRepository{getByIDForUser: func(context.Context, int64, int64) (*conversation.Conversation, error) {
				checked = true
				if tt.cancelAfterAccess {
					cancel()
				}
				return &conversation.Conversation{ID: 7}, tt.accessError
			}}
			repo := fakeConversationMessageRepository{list: func(context.Context, int64, *int64) (*conversation.MessagePage, error) {
				listed = true
				return &conversation.MessagePage{}, fmt.Errorf("repository: %w", tt.listError)
			}}
			got, err := service.NewConversationMessageService(access, repo, nil).List(ctx, 7, 42, tt.beforeID)
			if got != nil || !errors.Is(err, tt.want) || checked != tt.wantAccess || listed != tt.wantList {
				t.Fatalf("result = (%+v, %v), access/list=%v/%v; want %v, %v/%v", got, err, checked, listed, tt.want, tt.wantAccess, tt.wantList)
			}
		})
	}
}
