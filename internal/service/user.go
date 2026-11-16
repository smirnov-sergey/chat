package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/validation"
)

type UserRepository interface {
	GetByUsername(ctx context.Context, username string) (*user.User, error)
	GetByID(ctx context.Context, id int64) (*user.User, error)
}

type UserRegistrationStore interface {
	Create(ctx context.Context, u *user.User, s *session.Session) error
}

type PasswordService interface {
	Hash(password string) (string, error)
	Verify(password, encoded string) (bool, error)
}

type SessionCreator interface {
	Create(ctx context.Context, userID int64) (*session.Token, error)
}

type UserSession struct {
	User  *user.User
	Token *session.Token
}

type UserService struct {
	repository        UserRepository
	registrationStore UserRegistrationStore
	passwords         PasswordService
	sessions          SessionCreator
	sessionLifetime   time.Duration
}

func NewUserService(
	repository UserRepository,
	registrationStore UserRegistrationStore,
	passwords PasswordService,
	sessions SessionCreator,
	sessionLifetime time.Duration,
) (*UserService, error) {
	if sessionLifetime <= 0 {
		return nil, errors.New("session lifetime must be positive")
	}

	return &UserService{
		repository:        repository,
		registrationStore: registrationStore,
		passwords:         passwords,
		sessions:          sessions,
		sessionLifetime:   sessionLifetime,
	}, nil
}

func (s *UserService) Register(ctx context.Context, username, name, password string) (*UserSession, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if err := validation.ValidateUsername(username); err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	if err := validation.ValidateName(name); err != nil {
		return nil, err
	}

	if err := validation.ValidatePassword(password); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	passwordHash, err := s.passwords.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	u := &user.User{
		Username:     username,
		Name:         name,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	initialSession, token := newSession(0, now, s.sessionLifetime)
	if err := s.registrationStore.Create(ctx, u, initialSession); err != nil {
		return nil, err
	}

	return &UserSession{User: u, Token: token}, nil
}

func (s *UserService) Login(ctx context.Context, username, password string) (*UserSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	username = strings.ToLower(strings.TrimSpace(username))
	if err := validation.ValidateUsername(username); err != nil {
		return nil, user.ErrInvalidCredentials
	}

	if password == "" || !utf8.ValidString(password) || utf8.RuneCountInString(password) > 128 {
		return nil, user.ErrInvalidCredentials
	}

	u, err := s.repository.GetByUsername(ctx, username)
	if errors.Is(err, user.ErrNotFound) {
		return nil, user.ErrInvalidCredentials
	}

	if err != nil {
		return nil, fmt.Errorf("find user for login: %w", err)
	}

	if u.PasswordHash == "" {
		return nil, user.ErrInvalidCredentials
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	matches, err := s.passwords.Verify(password, u.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("verify password: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if !matches {
		return nil, user.ErrInvalidCredentials
	}

	token, err := s.sessions.Create(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("create login session: %w", err)
	}

	return &UserSession{User: u, Token: token}, nil
}

func (s *UserService) GetByID(ctx context.Context, id int64) (*user.User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	u, err := s.repository.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user by ID: %w", err)
	}

	return u, nil
}
