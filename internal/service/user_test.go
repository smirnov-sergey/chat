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

type fakeUserRepository struct {
	create func(context.Context, *user.User, string) error
}

func (repo fakeUserRepository) Create(ctx context.Context, u *user.User, hash string) error {
	return repo.create(ctx, u, hash)
}

type fakePasswordHasher func(string) (string, error)

func (hasher fakePasswordHasher) Hash(password string) (string, error) {
	return hasher(password)
}

const testPassword = "  LongPassword123!  "
const testPasswordHash = "test-password-hash"

func fixedPasswordHash(string) (string, error) {
	return testPasswordHash, nil
}

func unexpectedPasswordHasher(t *testing.T) fakePasswordHasher {
	t.Helper()
	return func(string) (string, error) {
		t.Fatal("hasher must not be called")
		return "", nil
	}
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
			hashCalls := 0
			var saved *user.User
			hasher := fakePasswordHasher(func(password string) (string, error) {
				hashCalls++
				if password != testPassword {
					t.Fatal("password must reach the hasher without trimming or case conversion")
				}
				return testPasswordHash, nil
			})
			repo := fakeUserRepository{create: func(gotCtx context.Context, u *user.User, hash string) error {
				calls++
				if hashCalls != 1 || hash != testPasswordHash {
					t.Fatal("repository must receive the hash after one hashing call")
				}
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
			got, err := service.NewUserService(repo, hasher).Create(ctx, tt.input, tt.inputName, testPassword)
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
			repo := fakeUserRepository{create: func(context.Context, *user.User, string) error {
				t.Fatal("repository must not be called for invalid input")
				return nil
			}}
			got, err := service.NewUserService(repo, unexpectedPasswordHasher(t)).Create(context.Background(), input, "Иван", testPassword)
			if got != nil || !errors.Is(err, validation.ErrInvalidUsername) {
				t.Fatalf("Create = (%+v, %v), want (nil, ErrInvalidUsername)", got, err)
			}
		})
	}
}

func TestUserServiceCreateRejectsInvalidName(t *testing.T) {
	for _, name := range []string{"", " \t\n", "\u00a0\u2003", strings.Repeat("Я", 81), "Иван\nПетров", "Иван\tПетров", "Иван\x00", "Иван\u2028Петров", "Иван\u2029Петров", "Иван\xff"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			repo := fakeUserRepository{create: func(context.Context, *user.User, string) error {
				t.Fatal("repository must not be called for invalid input")
				return nil
			}}
			got, err := service.NewUserService(repo, unexpectedPasswordHasher(t)).Create(context.Background(), "ivan", name, testPassword)
			if got != nil || !errors.Is(err, validation.ErrInvalidName) {
				t.Fatalf("Create = (%+v, %v), want (nil, ErrInvalidName)", got, err)
			}
		})
	}
}

func TestUserServiceCreateRepositoryErrors(t *testing.T) {
	for _, want := range []error{user.ErrUsernameTaken, errors.New("database unavailable")} {
		t.Run(want.Error(), func(t *testing.T) {
			repo := fakeUserRepository{create: func(context.Context, *user.User, string) error {
				return fmt.Errorf("save user: %w", want)
			}}
			got, err := service.NewUserService(repo, fakePasswordHasher(fixedPasswordHash)).Create(context.Background(), "Ivan", "Иван", testPassword)
			if got != nil || !errors.Is(err, want) {
				t.Fatalf("Create = (%+v, %v), want (nil, %v)", got, err, want)
			}
		})
	}
}

func TestUserServiceCreateCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := fakeUserRepository{create: func(context.Context, *user.User, string) error {
		t.Fatal("repository must not be called for an already canceled operation")
		return nil
	}}
	got, err := service.NewUserService(repo, unexpectedPasswordHasher(t)).Create(ctx, "ivan", "Иван", testPassword)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Create = (%+v, %v), want (nil, context.Canceled)", got, err)
	}
}

func TestUserServiceCreateRejectsInvalidPassword(t *testing.T) {
	for _, password := range []string{"", strings.Repeat("a", 14), strings.Repeat("Я", 129), "long-password-\xff"} {
		t.Run(fmt.Sprintf("length_%d", len(password)), func(t *testing.T) {
			repo := fakeUserRepository{create: func(context.Context, *user.User, string) error {
				t.Fatal("repository must not be called for invalid input")
				return nil
			}}
			got, err := service.NewUserService(repo, unexpectedPasswordHasher(t)).Create(t.Context(), "ivan", "Иван", password)
			if got != nil || !errors.Is(err, validation.ErrInvalidPassword) {
				t.Fatalf("Create = (%+v, %v), want (nil, ErrInvalidPassword)", got, err)
			}
		})
	}
}

func TestUserServiceCreateHashError(t *testing.T) {
	want := errors.New("hashing failed")
	hasher := fakePasswordHasher(func(string) (string, error) {
		return "", want
	})
	repo := fakeUserRepository{create: func(context.Context, *user.User, string) error {
		t.Fatal("repository must not be called after a hashing error")
		return nil
	}}
	got, err := service.NewUserService(repo, hasher).Create(t.Context(), "ivan", "Иван", testPassword)
	if got != nil || !errors.Is(err, want) {
		t.Fatalf("Create = (%+v, %v), want (nil, hashing error)", got, err)
	}
}
