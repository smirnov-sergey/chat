package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/validation"
)

type UserLoginRepository interface {
	GetByUsername(ctx context.Context, username string) (*user.User, error)
}

type PasswordVerifier interface {
	Verify(password, encoded string) (bool, error)
}

type SessionCreator interface {
	Create(ctx context.Context, userID int64) (*session.Token, error)
}

type UserLogin struct {
	User  *user.User
	Token *session.Token
}

type UserLoginService struct {
	repository       UserLoginRepository
	passwordVerifier PasswordVerifier
	sessions         SessionCreator
}

func NewUserLoginService(repository UserLoginRepository, passwordVerifier PasswordVerifier, sessions SessionCreator) *UserLoginService {
	return &UserLoginService{repository: repository, passwordVerifier: passwordVerifier, sessions: sessions}
}

func (s *UserLoginService) Login(ctx context.Context, username, password string) (*UserLogin, error) {
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

	matches, err := s.passwordVerifier.Verify(password, u.PasswordHash)
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

	return &UserLogin{User: u, Token: token}, nil
}
