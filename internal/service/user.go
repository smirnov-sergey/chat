package service

import (
	"context"
	"strings"
	"time"

	"chat/internal/model/user"
	"chat/internal/validation"
)

type UserRepository interface {
	Create(ctx context.Context, u *user.User) error
}

type UserService struct {
	repository UserRepository
}

func NewUserService(repository UserRepository) *UserService {
	return &UserService{repository: repository}
}

func (s *UserService) Create(ctx context.Context, username, name string) (*user.User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if err := validation.ValidateUsername(username); err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	if err := validation.ValidateName(name); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	u := &user.User{
		Username:  username,
		Name:      name,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repository.Create(ctx, u); err != nil {
		return nil, err
	}

	return u, nil
}
