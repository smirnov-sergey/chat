package repository

import (
	"context"
	"database/sql"
	"fmt"

	"chat/internal/model/session"
	"chat/internal/model/user"
)

type UserRegistrationStore struct {
	db *sql.DB
}

func NewUserRegistrationStore(db *sql.DB) *UserRegistrationStore {
	return &UserRegistrationStore{db: db}
}

func (store *UserRegistrationStore) Create(ctx context.Context, u *user.User, passwordHash string, s *session.Session) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin registration transaction: %w", err)
	}
	defer tx.Rollback()

	storedUser := *u
	if err := NewUserRepository(tx).Create(ctx, &storedUser, passwordHash); err != nil {
		return err
	}

	storedSession := *s
	storedSession.UserID = storedUser.ID
	if err := NewSessionRepository(tx).Create(ctx, &storedSession); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit registration transaction: %w", err)
	}

	u.ID = storedUser.ID
	s.UserID = storedSession.UserID

	return nil
}
