//go:build integration

package integration_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"chat/internal/migrate"
	"chat/internal/model/user"
	"chat/internal/repository"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestUserRepositoryUpdatePhoto(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migration directory")
	}
	files := os.DirFS(filepath.Join(filepath.Dir(source), "..", "..", "migrations"))
	if _, err := migrate.Up(ctx, db, files); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(db)
	now := time.Date(2026, 11, 17, 12, 0, 0, 123456000, time.UTC)
	u := user.User{Username: "ivan", Name: "Иван", PasswordHash: "password-hash", CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(ctx, &u); err != nil {
		t.Fatal(err)
	}
	first, second := "first.png", "second.png"
	for _, tt := range []struct {
		name        string
		photo       *string
		previous    *string
		requestedAt time.Time
		updatedAt   time.Time
	}{
		{"set first photo", &first, nil, now.Add(time.Minute), now.Add(time.Minute)},
		{"same photo preserves date", &first, &first, now.Add(2 * time.Minute), now.Add(time.Minute)},
		{"replace photo", &second, &first, now.Add(3 * time.Minute), now.Add(3 * time.Minute)},
		{"remove photo", nil, &second, now.Add(4 * time.Minute), now.Add(4 * time.Minute)},
		{"repeat removal preserves date", nil, nil, now.Add(5 * time.Minute), now.Add(4 * time.Minute)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			previous, err := repo.UpdatePhoto(ctx, u.ID, tt.photo, tt.requestedAt)
			if err != nil {
				t.Fatal(err)
			}
			if (previous == nil) != (tt.previous == nil) || (previous != nil && *previous != *tt.previous) {
				t.Fatal("UpdatePhoto must return the filename stored before the update")
			}
			stored, err := repo.GetByID(ctx, u.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (stored.Photo == nil) != (tt.photo == nil) || (stored.Photo != nil && *stored.Photo != *tt.photo) {
				t.Fatal("stored photo must match the requested value")
			}
			if !stored.UpdatedAt.Equal(tt.updatedAt) || !stored.CreatedAt.Equal(now) ||
				stored.Username != u.Username || stored.Name != u.Name || stored.PasswordHash != u.PasswordHash {
				t.Fatalf("unexpected profile after photo update: %+v", stored)
			}
		})
	}
	t.Run("failed updates preserve profile", func(t *testing.T) {
		empty := ""
		previous, err := repo.UpdatePhoto(ctx, u.ID, &empty, now.Add(6*time.Minute))
		var pgErr *pgconn.PgError
		if previous != nil || !errors.As(err, &pgErr) || pgErr.ConstraintName != "users_photo_not_empty" {
			t.Fatalf("empty photo error = %v, want wrapped check constraint violation", err)
		}
		canceled, stop := context.WithCancel(ctx)
		stop()
		if previous, err := repo.UpdatePhoto(canceled, u.ID, &first, now.Add(7*time.Minute)); previous != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled update error = %v, want context.Canceled", err)
		}
		if previous, err := repo.UpdatePhoto(ctx, 0, &first, now); previous != nil || !errors.Is(err, user.ErrNotFound) {
			t.Fatalf("missing user error = %v, want ErrNotFound", err)
		}
		stored, err := repo.GetByID(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Photo != nil || !stored.UpdatedAt.Equal(now.Add(4*time.Minute)) {
			t.Fatal("failed updates must preserve the photo and modification date")
		}
	})
	t.Run("concurrent replacements return distinct previous files", func(t *testing.T) {
		original := "original.png"
		if _, err := repo.UpdatePhoto(ctx, u.ID, &original, now.Add(8*time.Minute)); err != nil {
			t.Fatal(err)
		}
		photos := [2]string{"concurrent-first.png", "concurrent-second.png"}
		type updateResult struct {
			index    int
			previous *string
			err      error
		}
		start := make(chan struct{})
		ready := make(chan struct{}, 2)
		results := make(chan updateResult, 2)
		for i := range photos {
			go func() {
				ready <- struct{}{}
				<-start
				previous, err := repo.UpdatePhoto(ctx, u.ID, &photos[i], now.Add(9*time.Minute))
				results <- updateResult{index: i, previous: previous, err: err}
			}()
		}
		for range photos {
			<-ready
		}
		close(start)
		var updates [2]updateResult
		for range photos {
			result := <-results
			updates[result.index] = result
		}
		for _, result := range updates {
			if result.err != nil || result.previous == nil {
				t.Fatalf("concurrent update %d: previous=%v, error=%v", result.index, result.previous, result.err)
			}
		}
		firstIndex := 0
		if *updates[1].previous == original {
			firstIndex = 1
		}
		secondIndex := 1 - firstIndex
		if *updates[firstIndex].previous != original || *updates[secondIndex].previous != photos[firstIndex] {
			t.Fatal("each replacement must return its actual predecessor, including concurrent updates")
		}
		stored, err := repo.GetByID(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Photo == nil || *stored.Photo != photos[secondIndex] {
			t.Fatal("the last replacement must remain the current photo")
		}
	})
}
