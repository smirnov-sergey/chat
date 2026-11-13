package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"chat/internal/model/user"
	"chat/internal/validation"
)

type UserRepository interface {
	Create(ctx context.Context, u *user.User, passwordHash string) error
}

type PasswordHasher interface {
	Hash(password string) (string, error)
}

type UserService struct {
	repository     UserRepository
	passwordHasher PasswordHasher
}

func NewUserService(repository UserRepository, passwordHasher PasswordHasher) *UserService {
	return &UserService{repository: repository, passwordHasher: passwordHasher}
}

func (s *UserService) Create(ctx context.Context, username, name, password string) (*user.User, error) {
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

	now := time.Now().UTC()
	u := &user.User{
		Username:  username,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repository.Create(ctx, u, passwordHash); err != nil {
		return nil, err
	}

	return u, nil
}
