package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"chat/internal/model/user"
	"chat/internal/service"
)

func TestUserServiceGetByID(t *testing.T) {
	ctx := t.Context()
	want := &user.User{ID: 42, Username: "ivan", PasswordHash: testPasswordHash}
	calls := 0
	repo := fakeUserRepository{getByID: func(gotCtx context.Context, id int64) (*user.User, error) {
		calls++
		if gotCtx != ctx || id != want.ID {
			t.Fatal("lookup must receive the supplied user ID and context")
		}
		return want, nil
	}}
	s, err := service.NewUserService(repo, nil, nil, nil, nil, nil, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetByID(ctx, want.ID)
	if err != nil || got != want || calls != 1 {
		t.Fatalf("GetByID error = %v, calls = %d; want the stored user and one call", err, calls)
	}
}

func TestUserServiceGetByIDErrors(t *testing.T) {
	for _, want := range []error{user.ErrNotFound, errors.New("database unavailable")} {
		t.Run(want.Error(), func(t *testing.T) {
			repo := fakeUserRepository{getByID: func(context.Context, int64) (*user.User, error) {
				return nil, fmt.Errorf("lookup: %w", want)
			}}
			s, err := service.NewUserService(repo, nil, nil, nil, nil, nil, 7*24*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.GetByID(t.Context(), 42)
			if got != nil || !errors.Is(err, want) {
				t.Fatalf("GetByID error = %v, want nil user and %v", err, want)
			}
		})
	}
}

func TestUserServiceGetByIDCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	repo := fakeUserRepository{getByID: func(context.Context, int64) (*user.User, error) {
		t.Fatal("canceled lookup must not call the repository")
		return nil, nil
	}}
	s, err := service.NewUserService(repo, nil, nil, nil, nil, nil, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetByID(ctx, 42)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("GetByID error = %v, want nil user and context.Canceled", err)
	}
}
