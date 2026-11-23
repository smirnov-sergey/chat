package service

import (
	"context"
	"fmt"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/validation"
)

type ConversationMessageAccess interface {
	GetByIDForUser(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error)
}

type ConversationMessageRepository interface {
	Create(ctx context.Context, message *conversation.Message) error
}

type ConversationMessageService struct {
	access     ConversationMessageAccess
	repository ConversationMessageRepository
}

func NewConversationMessageService(access ConversationMessageAccess, repository ConversationMessageRepository) *ConversationMessageService {
	return &ConversationMessageService{access: access, repository: repository}
}

func (s *ConversationMessageService) Create(ctx context.Context, conversationID, currentUserID int64, text string) (*conversation.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if conversationID <= 0 || currentUserID <= 0 {
		return nil, conversation.ErrNotFound
	}

	if _, err := s.access.GetByIDForUser(ctx, conversationID, currentUserID); err != nil {
		return nil, fmt.Errorf("get conversation for message: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := validation.ValidateConversationMessageText(text); err != nil {
		return nil, err
	}

	message := &conversation.Message{
		ConversationID: conversationID,
		SenderUserID:   currentUserID,
		Text:           text,
		CreatedAt:      time.Now().UTC(),
	}

	if err := s.repository.Create(ctx, message); err != nil {
		return nil, fmt.Errorf("create conversation message: %w", err)
	}

	return message, nil
}
