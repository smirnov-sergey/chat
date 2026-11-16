package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/service"
	"chat/internal/validation"
)

type fakeUserRegistrationStore struct {
	create func(context.Context, *user.User, string, *session.Session) error
}

func (store fakeUserRegistrationStore) Create(ctx context.Context, u *user.User, hash string, s *session.Session) error {
	return store.create(ctx, u, hash, s)
}

var _ service.UserRegistrationStore = fakeUserRegistrationStore{}

func newUserRegistrationService(t *testing.T, store fakeUserRegistrationStore, hasher service.PasswordHasher) *service.UserRegistrationService {
	t.Helper()
	s, err := service.NewUserRegistrationService(store, hasher, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewUserRegistrationServiceRejectsInvalidLifetime(t *testing.T) {
	for _, lifetime := range []time.Duration{0, -time.Second} {
		s, err := service.NewUserRegistrationService(fakeUserRegistrationStore{}, fakePasswordHasher(fixedPasswordHash), lifetime)
		if s != nil || err == nil {
			t.Fatalf("NewUserRegistrationService(%v) = (%v, %v), want (nil, error)", lifetime, s, err)
		}
	}
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

func TestUserRegistrationServiceRegister(t *testing.T) {
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
			var savedSession *session.Session
			hasher := fakePasswordHasher(func(password string) (string, error) {
				hashCalls++
				if password != testPassword {
					t.Fatal("password must reach the hasher without trimming or case conversion")
				}
				return testPasswordHash, nil
			})
			repo := fakeUserRegistrationStore{create: func(gotCtx context.Context, u *user.User, hash string, initialSession *session.Session) error {
				calls++
				if hashCalls != 1 || hash != testPasswordHash {
					t.Fatal("store must receive the hash after one hashing call")
				}
				if gotCtx != ctx {
					t.Error("store must receive the caller's context")
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
				if initialSession.UserID != 0 || !initialSession.CreatedAt.Equal(u.CreatedAt) ||
					initialSession.ExpiresAt.Sub(initialSession.CreatedAt) != 7*24*time.Hour {
					t.Fatal("initial session must await the generated user ID and expire seven days after registration")
				}
				u.ID = 42
				initialSession.UserID = u.ID
				saved = u
				savedSession = initialSession
				return nil
			}}
			got, err := newUserRegistrationService(t, repo, hasher).Register(ctx, tt.input, tt.inputName, testPassword)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || got == nil || got.User != saved || got.User.ID != 42 || got.Token == nil {
				t.Fatalf("Register = %+v, calls = %d; want the saved user with ID 42 and one call", got, calls)
			}
			raw, err := base64.RawURLEncoding.Strict().DecodeString(got.Token.Value)
			if err != nil || len(raw) != 32 {
				t.Fatal("registration must return a URL-safe token containing 32 random bytes")
			}
			hash := sha256.Sum256(raw)
			if !bytes.Equal(savedSession.TokenHash, hash[:]) || !got.Token.ExpiresAt.Equal(savedSession.ExpiresAt) {
				t.Fatal("returned token must match the hash and expiration sent to the store")
			}
		})
	}
}

func TestUserRegistrationServiceRegisterRejectsInvalidUsername(t *testing.T) {
	for _, input := range []string{"", " \t\n", "AB", strings.Repeat("A", 33), "иван", "iv an", "iv-an", "ivan😀", "iv\x00an", "iv\xffan"} {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			repo := fakeUserRegistrationStore{create: func(context.Context, *user.User, string, *session.Session) error {
				t.Fatal("store must not be called for invalid input")
				return nil
			}}
			got, err := newUserRegistrationService(t, repo, unexpectedPasswordHasher(t)).Register(context.Background(), input, "Иван", testPassword)
			if got != nil || !errors.Is(err, validation.ErrInvalidUsername) {
				t.Fatalf("Register = (%+v, %v), want (nil, ErrInvalidUsername)", got, err)
			}
		})
	}
}

func TestUserRegistrationServiceRegisterRejectsInvalidName(t *testing.T) {
	for _, name := range []string{"", " \t\n", "\u00a0\u2003", strings.Repeat("Я", 81), "Иван\nПетров", "Иван\tПетров", "Иван\x00", "Иван\u2028Петров", "Иван\u2029Петров", "Иван\xff"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			repo := fakeUserRegistrationStore{create: func(context.Context, *user.User, string, *session.Session) error {
				t.Fatal("store must not be called for invalid input")
				return nil
			}}
			got, err := newUserRegistrationService(t, repo, unexpectedPasswordHasher(t)).Register(context.Background(), "ivan", name, testPassword)
			if got != nil || !errors.Is(err, validation.ErrInvalidName) {
				t.Fatalf("Register = (%+v, %v), want (nil, ErrInvalidName)", got, err)
			}
		})
	}
}

func TestUserRegistrationServiceRegisterStoreErrors(t *testing.T) {
	for _, want := range []error{user.ErrUsernameTaken, errors.New("database unavailable")} {
		t.Run(want.Error(), func(t *testing.T) {
			repo := fakeUserRegistrationStore{create: func(context.Context, *user.User, string, *session.Session) error {
				return fmt.Errorf("save user: %w", want)
			}}
			got, err := newUserRegistrationService(t, repo, fakePasswordHasher(fixedPasswordHash)).Register(context.Background(), "Ivan", "Иван", testPassword)
			if got != nil || !errors.Is(err, want) {
				t.Fatalf("Register = (%+v, %v), want (nil, %v)", got, err, want)
			}
		})
	}
}

func TestUserRegistrationServiceRegisterCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := fakeUserRegistrationStore{create: func(context.Context, *user.User, string, *session.Session) error {
		t.Fatal("store must not be called for an already canceled operation")
		return nil
	}}
	got, err := newUserRegistrationService(t, repo, unexpectedPasswordHasher(t)).Register(ctx, "ivan", "Иван", testPassword)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Register = (%+v, %v), want (nil, context.Canceled)", got, err)
	}
}

func TestUserRegistrationServiceRegisterRejectsInvalidPassword(t *testing.T) {
	for _, password := range []string{"", strings.Repeat("a", 14), strings.Repeat("Я", 129), "long-password-\xff"} {
		t.Run(fmt.Sprintf("length_%d", len(password)), func(t *testing.T) {
			repo := fakeUserRegistrationStore{create: func(context.Context, *user.User, string, *session.Session) error {
				t.Fatal("store must not be called for invalid input")
				return nil
			}}
			got, err := newUserRegistrationService(t, repo, unexpectedPasswordHasher(t)).Register(t.Context(), "ivan", "Иван", password)
			if got != nil || !errors.Is(err, validation.ErrInvalidPassword) {
				t.Fatalf("Register = (%+v, %v), want (nil, ErrInvalidPassword)", got, err)
			}
		})
	}
}

func TestUserRegistrationServiceRegisterHashError(t *testing.T) {
	want := errors.New("hashing failed")
	hasher := fakePasswordHasher(func(string) (string, error) {
		return "", want
	})
	repo := fakeUserRegistrationStore{create: func(context.Context, *user.User, string, *session.Session) error {
		t.Fatal("store must not be called after a hashing error")
		return nil
	}}
	got, err := newUserRegistrationService(t, repo, hasher).Register(t.Context(), "ivan", "Иван", testPassword)
	if got != nil || !errors.Is(err, want) {
		t.Fatalf("Register = (%+v, %v), want (nil, hashing error)", got, err)
	}
}

func TestUserRegistrationServiceRegisterCanceledDuringHashing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	hasher := fakePasswordHasher(func(string) (string, error) {
		cancel()
		return testPasswordHash, nil
	})
	store := fakeUserRegistrationStore{create: func(context.Context, *user.User, string, *session.Session) error {
		t.Fatal("store must not be called after cancellation during hashing")
		return nil
	}}
	got, err := newUserRegistrationService(t, store, hasher).Register(ctx, "ivan", "Иван", testPassword)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Register = (%+v, %v), want (nil, context.Canceled)", got, err)
	}
}
