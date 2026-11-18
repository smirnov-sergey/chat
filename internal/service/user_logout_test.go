package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"chat/internal/service"
)

func TestUserServiceLogout(t *testing.T) {
	for _, want := range []error{nil, errors.New("session deletion failed")} {
		ctx := t.Context()
		calls := 0
		sessions := fakeUserSessions{logout: func(gotCtx context.Context, token string) error {
			calls++
			if gotCtx != ctx || token != "raw-token" {
				t.Fatal("user service must pass the unchanged context and token to the session service")
			}
			return want
		}}
		s, err := service.NewUserService(nil, nil, nil, sessions, nil, nil, 7*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Logout(ctx, "raw-token"); !errors.Is(err, want) || calls != 1 {
			t.Fatalf("Logout error = %v, calls = %d; want %v, 1", err, calls, want)
		}
	}
}
