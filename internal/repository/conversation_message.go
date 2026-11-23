package repository

import (
	"context"
	"fmt"
	"time"

	"chat/internal/model/conversation"
)

type ConversationMessageRepository struct {
	db rowQueryer
}

func NewConversationMessageRepository(db rowQueryer) *ConversationMessageRepository {
	return &ConversationMessageRepository{db: db}
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
