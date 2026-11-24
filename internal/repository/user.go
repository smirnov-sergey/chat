package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"chat/internal/model/user"
)

type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type userQueryer interface {
	rowQueryer
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type UserRepository struct {
	db userQueryer
}

func NewUserRepository(db userQueryer) *UserRepository {
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
	row := repo.db.QueryRowContext(ctx, `
		SELECT id, username, name, created_at, updated_at, COALESCE(password_hash, ''), photo
		FROM users
		WHERE username = $1`, username,
	)
	u, err := scanUser(row)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, user.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get user by username: %w", err)
	}

	return u, nil
}

func (repo *UserRepository) GetByUsernames(ctx context.Context, usernames []string) ([]user.User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	users := make([]user.User, 0, len(usernames))
	if len(usernames) == 0 {
		return users, nil
	}

	placeholders := make([]string, len(usernames))
	args := make([]any, len(usernames))
	for i, username := range usernames {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = username
	}

	rows, err := repo.db.QueryContext(ctx, `
		SELECT id, username, name, created_at, updated_at, COALESCE(password_hash, ''), photo
		FROM users
		WHERE username IN (`+strings.Join(placeholders, ", ")+")", args...)
	if err != nil {
		return nil, fmt.Errorf("get users by usernames: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, *u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}

	return users, nil
}

func (repo *UserRepository) GetByID(ctx context.Context, id int64) (*user.User, error) {
	row := repo.db.QueryRowContext(ctx, `
		SELECT id, username, name, created_at, updated_at, COALESCE(password_hash, ''), photo
		FROM users
		WHERE id = $1`, id,
	)
	u, err := scanUser(row)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, user.ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get user by ID: %w", err)
	}

	return u, nil
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

func scanUser(row interface{ Scan(...any) error }) (*user.User, error) {
	var u user.User
	if err := row.Scan(
		&u.ID,
		&u.Username,
		&u.Name,
		&u.CreatedAt,
		&u.UpdatedAt,
		&u.PasswordHash,
		&u.Photo,
	); err != nil {
		return nil, err
	}
	return &u, nil
}
