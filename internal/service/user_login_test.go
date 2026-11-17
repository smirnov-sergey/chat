package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/repository"
	"chat/internal/service"
)

type fakeUserRepository struct {
	getByUsername func(context.Context, string) (*user.User, error)
	getByID       func(context.Context, int64) (*user.User, error)
}

func (fake fakeUserRepository) GetByUsername(ctx context.Context, username string) (*user.User, error) {
	return fake.getByUsername(ctx, username)
}

func (fake fakeUserRepository) GetByID(ctx context.Context, id int64) (*user.User, error) {
	return fake.getByID(ctx, id)
}

type fakePasswordVerifier func(string, string) (bool, error)

type fakeUserSessions struct {
	create func(context.Context, int64) (*session.Token, error)
	logout func(context.Context, string) error
}

func (fake fakeUserSessions) Create(ctx context.Context, userID int64) (*session.Token, error) {
	return fake.create(ctx, userID)
}

func (fake fakeUserSessions) Logout(ctx context.Context, token string) error {
	return fake.logout(ctx, token)
}

var (
	_ service.UserRepository = fakeUserRepository{}
	_ service.UserRepository = (*repository.UserRepository)(nil)
	_ service.UserSessions   = fakeUserSessions{}
	_ service.UserSessions   = (*service.SessionService)(nil)
)

func TestUserServiceLogin(t *testing.T) {
	for _, tt := range []struct {
		name     string
		username string
		password string
	}{
		{"unchanged input", "ivan", testPassword},
		{"outer whitespace and case", " \tIVAN\n", testPassword},
		{"Unicode whitespace", "\u00a0Ivan\u2003", testPassword},
		{"existing short password", "ivan", "short"},
		{"maximum Unicode length", "ivan", strings.Repeat("Я", 128)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			u := &user.User{ID: 42, Username: "ivan", PasswordHash: testPasswordHash}
			token := &session.Token{Value: "session-token", ExpiresAt: time.Now().Add(7 * 24 * time.Hour)}
			var calls []string
			repo := fakeUserRepository{getByUsername: func(gotCtx context.Context, gotUsername string) (*user.User, error) {
				if gotCtx != ctx || gotUsername != "ivan" {
					t.Fatal("lookup must receive the caller's context and normalized username")
				}
				calls = append(calls, "lookup")
				return u, nil
			}}
			verifier := fakePasswordVerifier(func(gotPassword, hash string) (bool, error) {
				if gotPassword != tt.password || hash != u.PasswordHash {
					t.Fatal("verification must receive the unchanged password and stored hash")
				}
				calls = append(calls, "verify")
				return true, nil
			})
			sessions := fakeUserSessions{create: func(gotCtx context.Context, id int64) (*session.Token, error) {
				if gotCtx != ctx || id != u.ID {
					t.Fatal("session creation must receive the authenticated user ID and caller's context")
				}
				calls = append(calls, "session")
				return token, nil
			}}
			got, err := newLoginUserService(t, repo, fakePasswordService{verify: verifier}, sessions).Login(ctx, tt.username, tt.password)
			if err != nil || got == nil || got.User != u || got.Token != token {
				t.Fatalf("Login error = %v; want the authenticated user and created token", err)
			}
			if strings.Join(calls, ",") != "lookup,verify,session" {
				t.Fatalf("calls = %v, want lookup, verify, session exactly once", calls)
			}
		})
	}
}

func TestUserServiceLoginFailures(t *testing.T) {
	lookupError := errors.New("database unavailable")
	verificationError := errors.New("unsupported hash")
	hashError := errors.New("hashing failed")
	sessionError := errors.New("session insert failed")
	for _, tt := range []struct {
		name              string
		username          string
		password          string
		hash              string
		lookupError       error
		verificationError error
		hashError         error
		sessionError      error
		matches           bool
		want              error
		wantCalls         string
	}{
		{name: "missing username", password: testPassword, want: user.ErrInvalidCredentials},
		{name: "invalid username", username: "iv-an", password: testPassword, want: user.ErrInvalidCredentials},
		{name: "empty password", username: "ivan", want: user.ErrInvalidCredentials},
		{name: "invalid UTF-8 password", username: "ivan", password: "password\xff", want: user.ErrInvalidCredentials},
		{name: "oversized password", username: "ivan", password: strings.Repeat("a", 129), want: user.ErrInvalidCredentials},
		{name: "missing user", username: "ivan", password: testPassword, lookupError: fmt.Errorf("lookup: %w", user.ErrNotFound), matches: true, want: user.ErrInvalidCredentials, wantCalls: "lookup,hash"},
		{name: "lookup error", username: "ivan", password: testPassword, lookupError: lookupError, want: lookupError, wantCalls: "lookup"},
		{name: "legacy user without password", username: "ivan", password: testPassword, matches: true, want: user.ErrInvalidCredentials, wantCalls: "lookup,hash"},
		{name: "hash error for missing user", username: "ivan", password: testPassword, lookupError: user.ErrNotFound, hashError: hashError, want: hashError, wantCalls: "lookup,hash"},
		{name: "wrong password", username: "ivan", password: testPassword, hash: testPasswordHash, want: user.ErrInvalidCredentials, wantCalls: "lookup,verify"},
		{name: "verification error", username: "ivan", password: testPassword, hash: testPasswordHash, verificationError: verificationError, want: verificationError, wantCalls: "lookup,verify"},
		{name: "session error", username: "ivan", password: testPassword, hash: testPasswordHash, matches: true, sessionError: sessionError, want: sessionError, wantCalls: "lookup,verify,session"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			repo := fakeUserRepository{getByUsername: func(context.Context, string) (*user.User, error) {
				calls = append(calls, "lookup")
				if tt.lookupError != nil {
					return nil, tt.lookupError
				}
				return &user.User{ID: 42, PasswordHash: tt.hash}, nil
			}}
			verifier := fakePasswordVerifier(func(string, string) (bool, error) {
				calls = append(calls, "verify")
				return tt.matches, tt.verificationError
			})
			passwords := fakePasswordService{
				hash: func(password string) (string, error) {
					calls = append(calls, "hash")
					if password != tt.password {
						t.Fatal("fallback hashing must receive the unchanged password")
					}
					return testPasswordHash, tt.hashError
				},
				verify: verifier,
			}
			sessions := fakeUserSessions{create: func(context.Context, int64) (*session.Token, error) {
				calls = append(calls, "session")
				return nil, tt.sessionError
			}}
			got, err := newLoginUserService(t, repo, passwords, sessions).Login(t.Context(), tt.username, tt.password)
			if got != nil || !errors.Is(err, tt.want) {
				t.Fatalf("Login error = %v, want nil result and %v", err, tt.want)
			}
			if strings.Join(calls, ",") != tt.wantCalls {
				t.Fatalf("calls = %v, want %q", calls, tt.wantCalls)
			}
		})
	}
}

func TestUserServiceLoginCancellation(t *testing.T) {
	for _, stage := range []string{"before lookup", "during lookup", "during verification"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls []string
			if stage == "before lookup" {
				cancel()
			}
			repo := fakeUserRepository{getByUsername: func(context.Context, string) (*user.User, error) {
				calls = append(calls, "lookup")
				if stage == "during lookup" {
					cancel()
				}
				return &user.User{ID: 42, PasswordHash: testPasswordHash}, nil
			}}
			verifier := fakePasswordVerifier(func(string, string) (bool, error) {
				calls = append(calls, "verify")
				cancel()
				return true, nil
			})
			sessions := fakeUserSessions{create: func(context.Context, int64) (*session.Token, error) {
				t.Fatal("canceled login must not create a session")
				return nil, nil
			}}
			got, err := newLoginUserService(t, repo, fakePasswordService{verify: verifier}, sessions).Login(ctx, "ivan", testPassword)
			if got != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("Login error = %v, want nil result and context.Canceled", err)
			}
			wantCalls := map[string]string{"before lookup": "", "during lookup": "lookup", "during verification": "lookup,verify"}[stage]
			if strings.Join(calls, ",") != wantCalls {
				t.Fatalf("calls = %v, want %q", calls, wantCalls)
			}
		})
	}
}

func newLoginUserService(t *testing.T, repo fakeUserRepository, passwords fakePasswordService, sessions fakeUserSessions) *service.UserService {
	t.Helper()
	s, err := service.NewUserService(repo, nil, passwords, sessions, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUserServiceLoginMissingCredentialsCancellation(t *testing.T) {
	for _, legacyUser := range []bool{false, true} {
		for _, stage := range []string{"lookup", "hash"} {
			t.Run(fmt.Sprintf("legacy_%v/during_%s", legacyUser, stage), func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				repo := fakeUserRepository{getByUsername: func(context.Context, string) (*user.User, error) {
					if stage == "lookup" {
						cancel()
					}
					if legacyUser {
						return &user.User{ID: 42}, nil
					}
					return nil, user.ErrNotFound
				}}
				hashCalls := 0
				passwords := fakePasswordService{hash: func(string) (string, error) {
					hashCalls++
					if stage == "lookup" {
						t.Fatal("cancellation during lookup must stop the fallback hash")
					}
					cancel()
					return testPasswordHash, nil
				}}
				sessions := fakeUserSessions{create: func(context.Context, int64) (*session.Token, error) {
					t.Fatal("missing credentials must never create a session")
					return nil, nil
				}}
				got, err := newLoginUserService(t, repo, passwords, sessions).Login(ctx, "ivan", testPassword)
				if got != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("Login error = %v, want nil result and context.Canceled", err)
				}
				if stage == "hash" && hashCalls != 1 {
					t.Fatalf("hash calls = %d, want 1", hashCalls)
				}
			})
		}
	}
}
