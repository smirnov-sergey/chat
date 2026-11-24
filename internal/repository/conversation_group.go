package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"chat/internal/model/conversation"
)

type ConversationGroupStore struct {
	db *sql.DB
}

func NewConversationGroupStore(db *sql.DB) *ConversationGroupStore {
	return &ConversationGroupStore{db: db}
}

func (store *ConversationGroupStore) Create(ctx context.Context, name string, ownerUserID int64, userIDs []int64, createdAt time.Time) (*conversation.Group, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin conversation group transaction: %w", err)
	}
	defer tx.Rollback()

	conversationRepository := NewConversationRepository(tx)
	group := conversation.Group{
		Conversation: conversation.Conversation{Type: conversation.TypeGroup, CreatedAt: createdAt},
		OwnerUserID:  ownerUserID,
		Name:         name,
	}

	if err := conversationRepository.Create(ctx, &group.Conversation); err != nil {
		return nil, err
	}

	if err := conversationRepository.createMembers(ctx, group.ID, userIDs); err != nil {
		return nil, err
	}

	if err := conversationRepository.createGroup(ctx, &group); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit group conversation: %w", err)
	}

	return &group, nil
}
