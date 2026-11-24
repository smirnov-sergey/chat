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
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type ConversationRepository struct {
	db conversationQueryer
}

func NewConversationRepository(db conversationQueryer) *ConversationRepository {
	return &ConversationRepository{db: db}
}

func (repo *ConversationRepository) lockByID(ctx context.Context, conversationID int64) error {
	var id int64

	err := repo.db.QueryRowContext(ctx, `SELECT id  FROM conversations WHERE id = $1 FOR NO KEY UPDATE`, conversationID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return conversation.ErrNotFound
	}

	if err != nil {
		return fmt.Errorf("lock conversation: %w", err)
	}

	return nil
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

	row := repo.db.QueryRowContext(ctx, `
		SELECT c.id, c.type, c.created_at
		FROM conversations c
			JOIN conversations_direct d ON d.conversation_id = c.id
		WHERE d.low_user_id = $1 AND d.high_user_id = $2`,
		lowUserID,
		highUserID,
	)
	c, err := scanConversation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get direct conversation by users: %w", err)
	}

	return c, nil
}

func (repo *ConversationRepository) GetPersonalByUser(ctx context.Context, userID int64) (*conversation.Conversation, error) {
	row := repo.db.QueryRowContext(ctx, `
		SELECT c.id, c.type, c.created_at
		FROM conversations c
			JOIN conversations_personal p ON p.conversation_id = c.id
		WHERE p.user_id = $1`,
		userID,
	)
	c, err := scanConversation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get personal conversation by user: %w", err)
	}

	return c, nil
}

func (repo *ConversationRepository) GetByIDForUser(ctx context.Context, conversationID, currentUserID int64) (*conversation.Conversation, error) {
	row := repo.db.QueryRowContext(ctx, `
		SELECT c.id, c.type, c.created_at
		FROM conversations c
			JOIN conversations_members m ON m.conversation_id = c.id
		WHERE c.id = $1 AND m.user_id = $2`,
		conversationID,
		currentUserID,
	)

	c, err := scanConversation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, conversation.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get conversation by ID for user: %w", err)
	}

	return c, nil
}

func (repo *ConversationRepository) ListByUser(ctx context.Context, currentUserID int64) ([]conversation.Conversation, error) {
	rows, err := repo.db.QueryContext(ctx, `
		SELECT c.id, c.type, c.created_at
		FROM conversations c
			JOIN conversations_members m ON m.conversation_id = c.id
		WHERE m.user_id = $1
		ORDER BY c.created_at DESC, c.id DESC`,
		currentUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("list user conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]conversation.Conversation, 0)
	for rows.Next() {
		var c conversation.Conversation
		if err := rows.Scan(&c.ID, &c.Type, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user conversation: %w", err)
		}
		conversations = append(conversations, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read user conversations: %w", err)
	}

	return conversations, nil
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

func (repo *ConversationRepository) createPersonal(ctx context.Context, conversationID, userID int64) error {
	var id int64

	err := repo.db.QueryRowContext(ctx, `
		INSERT INTO conversations_personal (conversation_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO NOTHING
		RETURNING conversation_id`,
		conversationID,
		userID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return conversation.ErrAlreadyExists
	}

	if err != nil {
		return fmt.Errorf("create personal conversation owner: %w", err)
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

func scanConversation(row *sql.Row) (*conversation.Conversation, error) {
	var c conversation.Conversation
	if err := row.Scan(&c.ID, &c.Type, &c.CreatedAt); err != nil {
		return nil, err
	}

	return &c, nil
}
