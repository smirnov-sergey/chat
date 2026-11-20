package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"chat/internal/model/conversation"
)

type ConversationDirectStore struct {
	db *sql.DB
}

func NewConversationDirectStore(db *sql.DB) *ConversationDirectStore {
	return &ConversationDirectStore{db: db}
}

func (store *ConversationDirectStore) Create(ctx context.Context, firstUserID, secondUserID int64, createdAt time.Time) (*conversation.Conversation, error) {
	if firstUserID <= 0 || secondUserID <= 0 || firstUserID == secondUserID {
		return nil, conversation.ErrInvalidParticipants
	}

	lowUserID := min(firstUserID, secondUserID)
	highUserID := max(firstUserID, secondUserID)

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin conversation direct transaction: %w", err)
	}
	defer tx.Rollback()

	repo := NewConversationRepository(tx)
	c := conversation.Conversation{
		Type:      conversation.TypeDirect,
		CreatedAt: createdAt,
	}

	if err := repo.Create(ctx, &c); err != nil {
		return nil, err
	}

	if err := repo.createDirect(ctx, c.ID, lowUserID, highUserID); err != nil {
		return nil, err
	}

	if err := repo.createMembers(ctx, c.ID, []int64{lowUserID, highUserID}); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit direct conversation: %w", err)
	}

	return &c, nil
}
