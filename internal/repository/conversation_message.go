package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"chat/internal/model/conversation"
)

const conversationMessagePageSize = 50

type conversationMessageQueryer interface {
	rowQueryer
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type ConversationMessageRepository struct {
	db conversationMessageQueryer
}

func NewConversationMessageRepository(db conversationMessageQueryer) *ConversationMessageRepository {
	return &ConversationMessageRepository{db: db}
}

func (repo *ConversationMessageRepository) List(ctx context.Context, conversationID int64, beforeID *int64) (*conversation.MessagePage, error) {
	query := `
		SELECT id, conversation_id, sender_user_id, text, created_at
		FROM conversations_messages
		WHERE conversation_id = $1`

	args := []any{conversationID}

	if beforeID != nil {
		query += " AND id < $2"
		args = append(args, *beforeID)
	}

	query += " ORDER BY id DESC LIMIT 51"

	rows, err := repo.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list conversation messages: %w", err)
	}
	defer rows.Close()

	page := &conversation.MessagePage{Messages: make([]conversation.Message, 0, conversationMessagePageSize+1)}
	for rows.Next() {
		var message conversation.Message
		if err := rows.Scan(&message.ID, &message.ConversationID, &message.SenderUserID, &message.Text, &message.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation message: %w", err)
		}
		page.Messages = append(page.Messages, message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read conversation messages: %w", err)
	}

	if len(page.Messages) > conversationMessagePageSize {
		page.Messages = page.Messages[:conversationMessagePageSize]
		nextCursor := page.Messages[len(page.Messages)-1].ID
		page.NextCursor = &nextCursor
	}

	return page, nil
}

func (repo *ConversationMessageRepository) Create(ctx context.Context, message *conversation.Message) error {
	var id int64
	var createdAt time.Time

	err := repo.db.QueryRowContext(ctx, `
		INSERT INTO conversations_messages (conversation_id, sender_user_id, text, created_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`,
		message.ConversationID,
		message.SenderUserID,
		message.Text,
		message.CreatedAt,
	).Scan(&id, &createdAt)
	if err != nil {
		return fmt.Errorf("create conversation message: %w", err)
	}

	message.ID = id
	message.CreatedAt = createdAt

	return nil
}
