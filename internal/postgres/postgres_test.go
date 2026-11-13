package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"chat/internal/postgres"
)

func TestOpenRejectsInvalidConnectionString(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	db, err := postgres.Open(ctx, "postgres://localhost:invalid/chat")
	if db != nil {
		db.Close()
		t.Fatal("expected nil pool on failure")
	}
	if err == nil {
		t.Fatal("expected connection configuration error")
	}
}

func TestOpenHonorsContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{
			name: "canceled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			want: context.Canceled,
		},
		{
			name: "deadline exceeded",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			want: context.DeadlineExceeded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.ctx()
			defer cancel()
			db, err := postgres.Open(ctx, "postgres://chat:password@127.0.0.1:5432/chat?sslmode=disable")
			if db != nil {
				db.Close()
				t.Fatal("expected nil pool on failure")
			}
			if !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
		})
	}
}
