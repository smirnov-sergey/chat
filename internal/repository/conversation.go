package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"chat/internal/model/conversation"
)

type conversationQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type ConversationRepository struct {
	db conversationQueryer
}

func NewConversationRepository(db conversationQueryer) *ConversationRepository {
	return &ConversationRepository{db: db}
}

func (repo *ConversationRepository) Create(ctx context.Context, c *conversation.Conversation) error {
	var id int64
	var createdAt time.Time

	err := repo.db.QueryRowContext(ctx, `
		INSERT INTO conversations (type, created_at)
		VALUES ($1, $2)
		RETURNING id, created_at`,
		c.Type,
		c.CreatedAt,
	).Scan(&id, &createdAt)

	if err != nil {
		return fmt.Errorf("create conversation: %w", err)
	}

	c.ID = id
	c.CreatedAt = createdAt

	return nil
}

func (repo *ConversationRepository) GetDirectByUsers(ctx context.Context, firstUserID, secondUserID int64) (*conversation.Conversation, error) {
	lowUserID := min(firstUserID, secondUserID)
	highUserID := max(firstUserID, secondUserID)

	var c conversation.Conversation
	err := repo.db.QueryRowContext(ctx, `
		SELECT c.id, c.type, c.created_at
		FROM conversations c
			JOIN conversations_direct d ON d.conversation_id = c.id
		WHERE d.low_user_id = $1 AND d.high_user_id = $2`,
		lowUserID,
		highUserID,
	).Scan(&c.ID, &c.Type, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get direct conversation by users: %w", err)
	}

	return &c, nil
}

func (repo *ConversationRepository) createDirect(ctx context.Context, conversationID, lowUserID, highUserID int64) error {
	var id int64

	err := repo.db.QueryRowContext(ctx, `
		INSERT INTO conversations_direct (conversation_id, low_user_id, high_user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (low_user_id, high_user_id) DO NOTHING
		RETURNING conversation_id`,
		conversationID,
		lowUserID,
		highUserID,
	).Scan(&id)

	if errors.Is(err, sql.ErrNoRows) {
		return conversation.ErrAlreadyExists
	}

	if err != nil {
		return fmt.Errorf("create direct conversation pair: %w", err)
	}

	return nil
}

func (repo *ConversationRepository) createMembers(ctx context.Context, conversationID int64, userIDs []int64) error {
	for _, userID := range userIDs {
		_, err := repo.db.ExecContext(ctx, `
			INSERT INTO conversations_members (conversation_id, user_id)
			VALUES ($1, $2)`,
			conversationID,
			userID,
		)

		if err != nil {
			return fmt.Errorf("create conversation member: %w", err)
		}
	}

	return nil
}
