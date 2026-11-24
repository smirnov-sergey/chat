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
	GetByUsernames(ctx context.Context, usernames []string) ([]user.User, error)
}

type ConversationRepository interface {
	ListByUser(ctx context.Context, currentUserID int64) ([]conversation.ListItem, error)
	GetByIDForUser(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error)
	GetDirectByUsers(ctx context.Context, firstUserID, secondUserID int64) (*conversation.Conversation, error)
	GetPersonalByUser(ctx context.Context, userID int64) (*conversation.Conversation, error)
	GetGroupByIDForUser(ctx context.Context, conversationID, currentUserID int64) (*conversation.Group, error)
}

type ConversationDirectStore interface {
	Create(ctx context.Context, firstUserID, secondUserID int64, createdAt time.Time) (*conversation.Conversation, error)
}

type ConversationPersonalStore interface {
	Create(ctx context.Context, userID int64, createdAt time.Time) (*conversation.Conversation, error)
}

type ConversationGroupStore interface {
	Create(ctx context.Context, name string, ownerUserID int64, userIDs []int64, createdAt time.Time) (*conversation.Group, error)
}

type ConversationService struct {
	users         ConversationUsers
	repository    ConversationRepository
	directStore   ConversationDirectStore
	personalStore ConversationPersonalStore
	groupStore    ConversationGroupStore
}

func NewConversationService(
	users ConversationUsers,
	repository ConversationRepository,
	directStore ConversationDirectStore,
	personalStore ConversationPersonalStore,
	groupStore ConversationGroupStore,
) *ConversationService {
	return &ConversationService{
		users:         users,
		repository:    repository,
		directStore:   directStore,
		personalStore: personalStore,
		groupStore:    groupStore,
	}
}

func (s *ConversationService) ListByUser(ctx context.Context, currentUserID int64) ([]conversation.ListItem, error) {
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

func (s *ConversationService) GetGroupByID(ctx context.Context, conversationID, currentUserID int64) (*conversation.Group, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if conversationID <= 0 || currentUserID <= 0 {
		return nil, conversation.ErrNotFound
	}

	group, err := s.repository.GetGroupByIDForUser(ctx, conversationID, currentUserID)
	if err != nil {
		return nil, fmt.Errorf("get group conversation for user: %w", err)
	}

	return group, nil
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

func (s *ConversationService) CreateGroup(ctx context.Context, currentUserID int64, name string, usernames []string) (*conversation.Group, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if currentUserID <= 0 {
		return nil, user.ErrNotFound
	}

	name = strings.TrimSpace(name)
	if err := validation.ValidateConversationGroupName(name); err != nil {
		return nil, err
	}

	if len(usernames) < 1 || len(usernames) > 49 {
		return nil, conversation.ErrInvalidGroupParticipants
	}

	normalized := make([]string, len(usernames))
	seenUsernames := make(map[string]struct{}, len(usernames))
	for i, username := range usernames {
		username = strings.ToLower(strings.TrimSpace(username))
		if err := validation.ValidateUsername(username); err != nil {
			return nil, err
		}

		if _, exists := seenUsernames[username]; exists {
			return nil, conversation.ErrInvalidGroupParticipants
		}

		seenUsernames[username] = struct{}{}
		normalized[i] = username
	}

	members, err := s.users.GetByUsernames(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("find group participants: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	membersByUsername := make(map[string]user.User, len(members))
	for _, member := range members {
		membersByUsername[member.Username] = member
	}

	userIDs := []int64{currentUserID}
	seenUserIDs := map[int64]struct{}{currentUserID: {}}
	for _, username := range normalized {
		member, exists := membersByUsername[username]
		if !exists {
			return nil, user.ErrNotFound
		}

		if _, exists := seenUserIDs[member.ID]; exists {
			return nil, conversation.ErrInvalidGroupParticipants
		}

		seenUserIDs[member.ID] = struct{}{}
		userIDs = append(userIDs, member.ID)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	group, err := s.groupStore.Create(ctx, name, currentUserID, userIDs, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("create group conversation: %w", err)
	}

	return group, nil
}
