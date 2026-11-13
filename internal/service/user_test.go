package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"chat/internal/model/user"
	"chat/internal/service"
	"chat/internal/validation"
)

type userRepositoryStub struct {
	create func(context.Context, *user.User) error
}

func (repo userRepositoryStub) Create(ctx context.Context, u *user.User) error {
	return repo.create(ctx, u)
}

func TestUserServiceCreate(t *testing.T) {
	for _, tt := range []struct {
		name        string
		input       string
		username    string
		inputName   string
		profileName string
	}{
		{"normalized", "ivan", "ivan", "Иван", "Иван"},
		{"uppercase and outer whitespace", " \tIVAN_123\n", "ivan_123", "  Иван Петров  ", "Иван Петров"},
		{"Unicode outer whitespace", "\u00a0Ivan\u2003", "ivan", "\u00a0Иван\u2003", "Иван"},
		{"minimum length", "ABC", "abc", "Я", "Я"},
		{"maximum length", strings.Repeat("A", 32), strings.Repeat("a", 32), strings.Repeat("Я", 80), strings.Repeat("Я", 80)},
		{"preserve case and inner spaces", "ivan", "ivan", "  Ivan  PETROV  ", "Ivan  PETROV"},
		{"emoji", "ivan", "ivan", "Иван 👨‍👩‍👧‍👦", "Иван 👨‍👩‍👧‍👦"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			before := time.Now().UTC()
			calls := 0
			var saved *user.User
			repo := userRepositoryStub{create: func(gotCtx context.Context, u *user.User) error {
				calls++
				if gotCtx != ctx {
					t.Error("repository must receive the caller's context")
				}
				if u.ID != 0 || u.Username != tt.username || u.Name != tt.profileName {
					t.Fatalf("user before insert = %+v, want zero ID, username %q and name %q", u, tt.username, tt.profileName)
				}
				if u.CreatedAt.IsZero() || !u.CreatedAt.Equal(u.UpdatedAt) || u.CreatedAt.Location() != time.UTC {
					t.Fatalf("creation timestamps must be equal, nonzero and UTC: %+v", u)
				}
				if u.CreatedAt.Before(before) || u.CreatedAt.After(time.Now().UTC()) {
					t.Fatalf("creation time %v is outside the operation interval", u.CreatedAt)
				}
				u.ID = 42
				saved = u
				return nil
			}}
			got, err := service.NewUserService(repo).Create(ctx, tt.input, tt.inputName)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || got != saved || got.ID != 42 {
				t.Fatalf("Create = %+v, calls = %d; want the saved user with ID 42 and one call", got, calls)
			}
		})
	}
}

func TestUserServiceCreateRejectsInvalidUsername(t *testing.T) {
	for _, input := range []string{"", " \t\n", "AB", strings.Repeat("A", 33), "иван", "iv an", "iv-an", "ivan😀", "iv\x00an", "iv\xffan"} {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			repo := userRepositoryStub{create: func(context.Context, *user.User) error {
				t.Fatal("repository must not be called for invalid input")
				return nil
			}}
			got, err := service.NewUserService(repo).Create(context.Background(), input, "Иван")
			if got != nil || !errors.Is(err, validation.ErrInvalidUsername) {
				t.Fatalf("Create = (%+v, %v), want (nil, ErrInvalidUsername)", got, err)
			}
		})
	}
}

func TestUserServiceCreateRejectsInvalidName(t *testing.T) {
	for _, name := range []string{"", " \t\n", "\u00a0\u2003", strings.Repeat("Я", 81), "Иван\nПетров", "Иван\tПетров", "Иван\x00", "Иван\u2028Петров", "Иван\u2029Петров", "Иван\xff"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			repo := userRepositoryStub{create: func(context.Context, *user.User) error {
				t.Fatal("repository must not be called for invalid input")
				return nil
			}}
			got, err := service.NewUserService(repo).Create(context.Background(), "ivan", name)
			if got != nil || !errors.Is(err, validation.ErrInvalidName) {
				t.Fatalf("Create = (%+v, %v), want (nil, ErrInvalidName)", got, err)
			}
		})
	}
}

func TestUserServiceCreateRepositoryErrors(t *testing.T) {
	for _, want := range []error{user.ErrUsernameTaken, errors.New("database unavailable")} {
		t.Run(want.Error(), func(t *testing.T) {
			repo := userRepositoryStub{create: func(context.Context, *user.User) error {
				return fmt.Errorf("save user: %w", want)
			}}
			got, err := service.NewUserService(repo).Create(context.Background(), "Ivan", "Иван")
			if got != nil || !errors.Is(err, want) {
				t.Fatalf("Create = (%+v, %v), want (nil, %v)", got, err, want)
			}
		})
	}
}

func TestUserServiceCreateCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := userRepositoryStub{create: func(gotCtx context.Context, _ *user.User) error {
		return gotCtx.Err()
	}}
	got, err := service.NewUserService(repo).Create(ctx, "ivan", "Иван")
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Create = (%+v, %v), want (nil, context.Canceled)", got, err)
	}
}
