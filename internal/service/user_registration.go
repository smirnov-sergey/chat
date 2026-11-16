package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/validation"
)

type UserRegistrationStore interface {
	Create(ctx context.Context, u *user.User, s *session.Session) error
}

type PasswordHasher interface {
	Hash(password string) (string, error)
}

type UserRegistration struct {
	User  *user.User
	Token *session.Token
}

type UserRegistrationService struct {
	store           UserRegistrationStore
	passwordHasher  PasswordHasher
	sessionLifetime time.Duration
}

func NewUserRegistrationService(store UserRegistrationStore, passwordHasher PasswordHasher, sessionLifetime time.Duration) (*UserRegistrationService, error) {
	if sessionLifetime <= 0 {
		return nil, errors.New("session lifetime must be positive")
	}
	return &UserRegistrationService{
		store:           store,
		passwordHasher:  passwordHasher,
		sessionLifetime: sessionLifetime,
	}, nil
}

func (s *UserRegistrationService) Register(ctx context.Context, username, name, password string) (*UserRegistration, error) {
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

	passwordHash, err := s.passwordHasher.Hash(password)
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
	if err := s.store.Create(ctx, u, initialSession); err != nil {
		return nil, err
	}

	return &UserRegistration{User: u, Token: token}, nil
}
