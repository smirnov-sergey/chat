package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"chat/internal/model/user"
)

type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type UserRepository struct {
	db rowQueryer
}

func NewUserRepository(db rowQueryer) *UserRepository {
	return &UserRepository{db: db}
}

func (repo *UserRepository) Create(ctx context.Context, u *user.User) error {
	var id int64

	err := repo.db.QueryRowContext(ctx, `
		INSERT INTO users (
			username,
			name,
			created_at,
			updated_at,
			password_hash,
			photo
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (username) DO NOTHING
		RETURNING id`,
		u.Username,
		u.Name,
		u.CreatedAt,
		u.UpdatedAt,
		u.PasswordHash,
		u.Photo,
	).Scan(&id)

	if errors.Is(err, sql.ErrNoRows) {
		return user.ErrUsernameTaken
	}

	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	u.ID = id
	return nil
}

func (repo *UserRepository) GetByUsername(ctx context.Context, username string) (*user.User, error) {
	var u user.User
	err := repo.db.QueryRowContext(ctx, `
		SELECT id, username, name, created_at, updated_at, COALESCE(password_hash, ''), photo
		FROM users
		WHERE username = $1`, username,
	).Scan(
		&u.ID,
		&u.Username,
		&u.Name,
		&u.CreatedAt,
		&u.UpdatedAt,
		&u.PasswordHash,
		&u.Photo,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, user.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get user by username: %w", err)
	}

	return &u, nil
}

func (repo *UserRepository) GetByID(ctx context.Context, id int64) (*user.User, error) {
	var u user.User

	err := repo.db.QueryRowContext(ctx, `
		SELECT id, username, name, created_at, updated_at, COALESCE(password_hash, ''), photo
		FROM users
		WHERE id = $1`, id,
	).Scan(
		&u.ID,
		&u.Username,
		&u.Name,
		&u.CreatedAt,
		&u.UpdatedAt,
		&u.PasswordHash,
		&u.Photo,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, user.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get user by ID: %w", err)
	}

	return &u, nil
}

func (repo *UserRepository) UpdatePhoto(ctx context.Context, id int64, photo *string, updatedAt time.Time) (*string, error) {
	var previous *string

	err := repo.db.QueryRowContext(ctx, `
		UPDATE users
		SET photo = $2,
			updated_at = CASE
				WHEN photo IS DISTINCT FROM $2 THEN $3
				ELSE updated_at
			END
		WHERE id = $1
		RETURNING old.photo`, id, photo, updatedAt).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, user.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("update user photo: %w", err)
	}

	return previous, nil
}
