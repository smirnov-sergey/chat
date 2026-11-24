package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"chat/internal/model/conversation"
)

type ConversationMessageStore struct {
	db *sql.DB
}

func NewConversationMessageStore(db *sql.DB) *ConversationMessageStore {
	return &ConversationMessageStore{db: db}
}

func (store *ConversationMessageStore) Create(ctx context.Context, conversationID, currentUserID int64, text string) (*conversation.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if conversationID <= 0 || currentUserID <= 0 {
		return nil, conversation.ErrNotFound
	}

	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin conversation message transaction: %w", err)
	}
	defer tx.Rollback()

	conversationRepository := NewConversationRepository(tx)
	if err := conversationRepository.lockByID(ctx, conversationID); err != nil {
		return nil, err
	}

	if _, err := conversationRepository.GetByIDForUser(ctx, conversationID, currentUserID); err != nil {
		return nil, err
	}

	message := &conversation.Message{
		ConversationID: conversationID,
		SenderUserID:   currentUserID,
		Text:           text,
		CreatedAt:      time.Now().UTC(),
	}

	if err := NewConversationMessageRepository(tx).Create(ctx, message); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit conversation message: %w", err)
	}

	return message, nil
}
