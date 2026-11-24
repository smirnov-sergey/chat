package service

import (
	"context"
	"fmt"

	"chat/internal/model/conversation"
	"chat/internal/validation"
)

type ConversationMessageAccess interface {
	GetByIDForUser(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error)
}

type ConversationMessageRepository interface {
	List(ctx context.Context, conversationID int64, beforeID *int64) (*conversation.MessagePage, error)
}

type ConversationMessageStore interface {
	Create(ctx context.Context, conversationID, currentUserID int64, text string) (*conversation.Message, error)
}

type ConversationMessageService struct {
	access     ConversationMessageAccess
	repository ConversationMessageRepository
	store      ConversationMessageStore
}

func NewConversationMessageService(access ConversationMessageAccess, repository ConversationMessageRepository, store ConversationMessageStore) *ConversationMessageService {
	return &ConversationMessageService{access: access, repository: repository, store: store}
}

func (s *ConversationMessageService) List(ctx context.Context, conversationID, currentUserID int64, beforeID *int64) (*conversation.MessagePage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if conversationID <= 0 || currentUserID <= 0 {
		return nil, conversation.ErrNotFound
	}

	if _, err := s.access.GetByIDForUser(ctx, conversationID, currentUserID); err != nil {
		return nil, fmt.Errorf("get conversation for message history: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if beforeID != nil && *beforeID <= 0 {
		return nil, conversation.ErrInvalidCursor
	}

	page, err := s.repository.List(ctx, conversationID, beforeID)
	if err != nil {
		return nil, fmt.Errorf("list conversation messages: %w", err)
	}

	return page, nil
}

func (s *ConversationMessageService) Create(ctx context.Context, conversationID, currentUserID int64, text string) (*conversation.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if conversationID <= 0 || currentUserID <= 0 {
		return nil, conversation.ErrNotFound
	}

	if err := validation.ValidateConversationMessageText(text); err != nil {
		return nil, err
	}

	message, err := s.store.Create(ctx, conversationID, currentUserID, text)
	if err != nil {
		return nil, fmt.Errorf("create conversation message: %w", err)
	}

	return message, nil
}
