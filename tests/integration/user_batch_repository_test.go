//go:build integration

package integration_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"chat/internal/model/user"
	"chat/internal/repository"
)

func TestUserRepositoryGetByUsernames(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	db, ids := conversationTestDB(t, ctx)
	repo := repository.NewUserRepository(db)
	for _, tt := range []struct {
		name      string
		usernames []string
		want      map[string]int64
	}{
		{"multiple users", []string{"anna", "ivan"}, map[string]int64{"ivan": ids[0], "anna": ids[1]}},
		{"partial result", []string{"ivan", "missing"}, map[string]int64{"ivan": ids[0]}},
		{"no matches", []string{"missing"}, map[string]int64{}},
		{"empty input", nil, map[string]int64{}},
		{"duplicate input", []string{"ivan", "ivan"}, map[string]int64{"ivan": ids[0]}},
		{"exact and parameterized", []string{"IVAN", " ivan ", "ivan') OR true --"}, map[string]int64{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			users, err := repo.GetByUsernames(ctx, tt.usernames)
			if err != nil || users == nil || len(users) != len(tt.want) {
				t.Fatalf("users = (%+v, %v), want %d rows", users, err, len(tt.want))
			}
			for _, got := range users {
				id, exists := tt.want[got.Username]
				if !exists || got.ID != id {
					t.Fatalf("unexpected user: %+v", got)
				}
				want, err := repo.GetByID(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, *want) {
					t.Fatalf("user = %+v, want %+v", got, want)
				}
			}
		})
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	for _, names := range [][]string{nil, {"ivan"}} {
		got, err := repo.GetByUsernames(canceled, names)
		if got != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled lookup = (%+v, %v)", got, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByUsernames(ctx, []string{"ivan"})
	if got != nil || err == nil || errors.Is(err, user.ErrNotFound) {
		t.Fatalf("closed database lookup = (%+v, %v)", got, err)
	}
}
