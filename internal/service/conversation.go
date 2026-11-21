package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/model/user"
	"chat/internal/validation"
)

type ConversationUsers interface {
	GetByUsername(ctx context.Context, username string) (*user.User, error)
}

type ConversationRepository interface {
	ListByUser(ctx context.Context, currentUserID int64) ([]conversation.Conversation, error)
	GetByIDForUser(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error)
	GetDirectByUsers(ctx context.Context, firstUserID, secondUserID int64) (*conversation.Conversation, error)
	GetPersonalByUser(ctx context.Context, userID int64) (*conversation.Conversation, error)
}

type ConversationDirectStore interface {
	Create(ctx context.Context, firstUserID, secondUserID int64, createdAt time.Time) (*conversation.Conversation, error)
}

type ConversationPersonalStore interface {
	Create(ctx context.Context, userID int64, createdAt time.Time) (*conversation.Conversation, error)
}

type ConversationService struct {
	users         ConversationUsers
	repository    ConversationRepository
	directStore   ConversationDirectStore
	personalStore ConversationPersonalStore
}

func NewConversationService(
	users ConversationUsers,
	repository ConversationRepository,
	directStore ConversationDirectStore,
	personalStore ConversationPersonalStore,
) *ConversationService {
	return &ConversationService{
		users:         users,
		repository:    repository,
		directStore:   directStore,
		personalStore: personalStore,
	}
}

func (s *ConversationService) ListByUser(ctx context.Context, currentUserID int64) ([]conversation.Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if currentUserID <= 0 {
		return nil, user.ErrNotFound
	}

	conversations, err := s.repository.ListByUser(ctx, currentUserID)
	if err != nil {
		return nil, fmt.Errorf("list user conversations: %w", err)
	}

	return conversations, nil
}

func (s *ConversationService) GetByID(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if conversationID <= 0 || currentUserID <= 0 {
		return nil, conversation.ErrNotFound
	}

	c, err := s.repository.GetByIDForUser(ctx, conversationID, currentUserID)
	if err != nil {
		return nil, fmt.Errorf("get conversation for user: %w", err)
	}

	return c, nil
}

func (s *ConversationService) CreateDirect(ctx context.Context, currentUserID int64, recipientUsername string) (*conversation.Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if currentUserID <= 0 {
		return nil, conversation.ErrInvalidParticipants
	}

	recipientUsername = strings.ToLower(strings.TrimSpace(recipientUsername))
	if err := validation.ValidateUsername(recipientUsername); err != nil {
		return nil, err
	}

	recipient, err := s.users.GetByUsername(ctx, recipientUsername)
	if err != nil {
		return nil, fmt.Errorf("find conversation user: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if currentUserID == recipient.ID {
		return nil, conversation.ErrInvalidParticipants
	}

	c, err := s.directStore.Create(ctx, currentUserID, recipient.ID, time.Now().UTC())
	if errors.Is(err, conversation.ErrAlreadyExists) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		c, err = s.repository.GetDirectByUsers(ctx, currentUserID, recipient.ID)
		if err != nil {
			return nil, fmt.Errorf("get existing direct conversation: %w", err)
		}

		return c, nil
	}

	if err != nil {
		return nil, fmt.Errorf("create direct conversation: %w", err)
	}

	return c, nil
}

func (s *ConversationService) CreatePersonal(ctx context.Context, currentUserID int64) (*conversation.Conversation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if currentUserID <= 0 {
		return nil, user.ErrNotFound
	}

	c, err := s.personalStore.Create(ctx, currentUserID, time.Now().UTC())
	if errors.Is(err, conversation.ErrAlreadyExists) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		c, err = s.repository.GetPersonalByUser(ctx, currentUserID)
		if err != nil {
			return nil, fmt.Errorf("get existing personal conversation: %w", err)
		}

		return c, nil
	}

	if err != nil {
		return nil, fmt.Errorf("create personal conversation: %w", err)
	}

	return c, nil
}
