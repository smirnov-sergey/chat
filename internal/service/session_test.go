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
	"chat/internal/service"
)

type fakeSessionRepository struct {
	create func(context.Context, *session.Session) error
	get    func(context.Context, []byte) (*session.Session, error)
	delete func(context.Context, []byte) error
}

func (repo fakeSessionRepository) Create(ctx context.Context, s *session.Session) error {
	return repo.create(ctx, s)
}

func (repo fakeSessionRepository) GetByTokenHash(ctx context.Context, hash []byte) (*session.Session, error) {
	return repo.get(ctx, hash)
}

func (repo fakeSessionRepository) DeleteByTokenHash(ctx context.Context, hash []byte) error {
	return repo.delete(ctx, hash)
}

func newSessionService(t *testing.T, repo fakeSessionRepository) *service.SessionService {
	t.Helper()
	s, err := service.NewSessionService(repo, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewSessionServiceRejectsInvalidLifetime(t *testing.T) {
	for _, lifetime := range []time.Duration{0, -time.Second} {
		s, err := service.NewSessionService(fakeSessionRepository{}, lifetime)
		if s != nil || err == nil {
			t.Fatalf("NewSessionService(%v) = (%v, %v), want (nil, error)", lifetime, s, err)
		}
	}
}

func TestSessionServiceCreate(t *testing.T) {
	ctx := t.Context()
	before := time.Now().UTC()
	var saved []*session.Session
	repo := fakeSessionRepository{create: func(gotCtx context.Context, value *session.Session) error {
		if gotCtx != ctx {
			t.Fatal("repository must receive the caller's context")
		}
		saved = append(saved, value)
		return nil
	}}
	s := newSessionService(t, repo)
	var previousToken string
	for i := range 2 {
		token, err := s.Create(ctx, 42)
		if err != nil {
			t.Fatal(err)
		}
		if token == nil || len(saved) != i+1 {
			t.Fatal("creation must return a token after one repository call")
		}
		raw, err := base64.RawURLEncoding.Strict().DecodeString(token.Value)
		if err != nil || len(raw) != 32 {
			t.Fatal("expected an unpadded URL-safe token containing 32 random bytes")
		}
		hash := sha256.Sum256(raw)
		stored := saved[i]
		if stored.UserID != 42 || !bytes.Equal(stored.TokenHash, hash[:]) {
			t.Fatal("repository must receive the user ID and token hash")
		}
		if stored.CreatedAt.Location() != time.UTC || stored.CreatedAt.Before(before) || stored.CreatedAt.After(time.Now().UTC()) {
			t.Fatal("creation time must be UTC and within the operation interval")
		}
		if stored.ExpiresAt.Sub(stored.CreatedAt) != 7*24*time.Hour || !token.ExpiresAt.Equal(stored.ExpiresAt) {
			t.Fatal("token and stored session must expire seven days after creation")
		}
		if i > 0 && (token.Value == previousToken || bytes.Equal(saved[0].TokenHash, stored.TokenHash)) {
			t.Fatal("each session must receive a new random token")
		}
		previousToken = token.Value
	}
}

func TestSessionServiceAuthenticate(t *testing.T) {
	ctx := t.Context()
	raw := bytes.Repeat([]byte{1}, 32)
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256(raw)
	want := &session.Session{TokenHash: hash[:], UserID: 42, ExpiresAt: time.Now().Add(time.Hour)}
	repo := fakeSessionRepository{get: func(gotCtx context.Context, gotHash []byte) (*session.Session, error) {
		if gotCtx != ctx || !bytes.Equal(gotHash, hash[:]) {
			t.Fatal("lookup must receive the caller's context and token hash")
		}
		return want, nil
	}}
	got, err := newSessionService(t, repo).Authenticate(ctx, token)
	if err != nil || got != want {
		t.Fatalf("Authenticate = (%+v, %v), want the stored session", got, err)
	}
}

func TestSessionServiceRejectsInvalidTokens(t *testing.T) {
	valid := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, token := range []string{"", "short", valid + "=", " " + valid, strings.Repeat("!", 43), valid[:42] + "B"} {
		t.Run(fmt.Sprintf("%q", token), func(t *testing.T) {
			// No repository method may be called for a malformed token.
			s := newSessionService(t, fakeSessionRepository{})
			got, err := s.Authenticate(t.Context(), token)
			if got != nil || !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("Authenticate = (%+v, %v), want (nil, ErrInvalid)", got, err)
			}
			if err := s.Logout(t.Context(), token); err != nil {
				t.Fatalf("Logout with an invalid token = %v, want nil", err)
			}
		})
	}
}

func TestSessionServiceAuthenticateRejectsMissingAndExpiredSessions(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, tt := range []struct {
		name  string
		value *session.Session
		err   error
	}{
		{"missing", nil, fmt.Errorf("lookup: %w", session.ErrNotFound)},
		{"expired", &session.Session{ExpiresAt: time.Now().Add(-time.Hour)}, nil},
		{"expiration reached", &session.Session{ExpiresAt: time.Now()}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := fakeSessionRepository{get: func(context.Context, []byte) (*session.Session, error) {
				return tt.value, tt.err
			}}
			got, err := newSessionService(t, repo).Authenticate(t.Context(), token)
			if got != nil || !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("Authenticate = (%+v, %v), want (nil, ErrInvalid)", got, err)
			}
		})
	}
}

func TestSessionServiceLogout(t *testing.T) {
	ctx := t.Context()
	raw := bytes.Repeat([]byte{1}, 32)
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256(raw)
	calls := 0
	repo := fakeSessionRepository{delete: func(gotCtx context.Context, gotHash []byte) error {
		calls++
		if gotCtx != ctx || !bytes.Equal(gotHash, hash[:]) {
			t.Fatal("deletion must receive the caller's context and token hash")
		}
		return nil
	}}
	s := newSessionService(t, repo)
	for range 2 {
		if err := s.Logout(ctx, token); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("deletion calls = %d, want 2", calls)
	}
}

func TestSessionServiceRepositoryErrors(t *testing.T) {
	want := errors.New("database unavailable")
	repo := fakeSessionRepository{
		create: func(context.Context, *session.Session) error { return want },
		get:    func(context.Context, []byte) (*session.Session, error) { return nil, want },
		delete: func(context.Context, []byte) error { return want },
	}
	s := newSessionService(t, repo)
	token := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	if got, err := s.Create(t.Context(), 42); got != nil || !errors.Is(err, want) {
		t.Fatalf("Create = (%+v, %v), want (nil, repository error)", got, err)
	}
	if got, err := s.Authenticate(t.Context(), token); got != nil || !errors.Is(err, want) {
		t.Fatalf("Authenticate = (%+v, %v), want (nil, repository error)", got, err)
	}
	if err := s.Logout(t.Context(), token); !errors.Is(err, want) {
		t.Fatalf("Logout = %v, want repository error", err)
	}
}

func TestSessionServiceCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := newSessionService(t, fakeSessionRepository{})
	if got, err := s.Create(ctx, 42); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Create = (%+v, %v), want (nil, context.Canceled)", got, err)
	}
	if got, err := s.Authenticate(ctx, ""); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Authenticate = (%+v, %v), want (nil, context.Canceled)", got, err)
	}
	if err := s.Logout(ctx, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("Logout = %v, want context.Canceled", err)
	}
}
