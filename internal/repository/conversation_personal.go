package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"chat/internal/model/conversation"
	"chat/internal/model/user"
)

type ConversationPersonalStore struct {
	db *sql.DB
}

func NewConversationPersonalStore(db *sql.DB) *ConversationPersonalStore {
	return &ConversationPersonalStore{db: db}
}

func (store *ConversationPersonalStore) Create(ctx context.Context, userID int64, createdAt time.Time) (*conversation.Conversation, error) {
	if userID <= 0 {
		return nil, user.ErrNotFound
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin conversation personal transaction: %w", err)
	}
	defer tx.Rollback()

	repo := NewConversationRepository(tx)
	c := conversation.Conversation{
		Type:      conversation.TypePersonal,
		CreatedAt: createdAt,
	}

	if err := repo.Create(ctx, &c); err != nil {
		return nil, err
	}

	if err := repo.createPersonal(ctx, c.ID, userID); err != nil {
		return nil, err
	}

	if err := repo.createMembers(ctx, c.ID, []int64{userID}); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit personal conversation: %w", err)
	}

	return &c, nil
}
