package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"chat/internal/model/session"
)

type sessionQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type SessionRepository struct {
	db sessionQueryer
}

func NewSessionRepository(db sessionQueryer) *SessionRepository {
	return &SessionRepository{db: db}
}

func (repo *SessionRepository) Create(ctx context.Context, s *session.Session) error {
	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO sessions (
			token_hash,
			user_id,
			created_at,
			expires_at
		)
		VALUES ($1, $2, $3, $4)`,
		s.TokenHash,
		s.UserID,
		s.CreatedAt,
		s.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	return nil
}

func (repo *SessionRepository) GetByTokenHash(ctx context.Context, tokenHash []byte) (*session.Session, error) {
	var s session.Session
	err := repo.db.QueryRowContext(ctx, `
		SELECT token_hash, user_id, created_at, expires_at
		FROM sessions
		WHERE token_hash = $1`, tokenHash,
	).Scan(
		&s.TokenHash,
		&s.UserID,
		&s.CreatedAt,
		&s.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, session.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get session by token hash: %w", err)
	}

	return &s, nil
}

func (repo *SessionRepository) DeleteByTokenHash(ctx context.Context, tokenHash []byte) error {
	if _, err := repo.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = $1", tokenHash); err != nil {
		return fmt.Errorf("delete session by token hash: %w", err)
	}

	return nil
}
