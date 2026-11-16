package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"chat/internal/model/session"
)

const sessionTokenLength = 32

type SessionRepository interface {
	Create(ctx context.Context, s *session.Session) error
	GetByTokenHash(ctx context.Context, tokenHash []byte) (*session.Session, error)
	DeleteByTokenHash(ctx context.Context, tokenHash []byte) error
}

type SessionService struct {
	repository SessionRepository
	lifetime   time.Duration
}

func NewSessionService(repository SessionRepository, lifetime time.Duration) (*SessionService, error) {
	if lifetime <= 0 {
		return nil, errors.New("session lifetime must be positive")
	}
	return &SessionService{repository: repository, lifetime: lifetime}, nil
}

func (s *SessionService) Create(ctx context.Context, userID int64) (*session.Token, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	value, token := newSession(userID, time.Now().UTC(), s.lifetime)

	if err := s.repository.Create(ctx, value); err != nil {
		return nil, err
	}

	return token, nil
}

func (s *SessionService) Authenticate(ctx context.Context, token string) (*session.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	hash, ok := sessionTokenHash(token)
	if !ok {
		return nil, session.ErrInvalid
	}

	value, err := s.repository.GetByTokenHash(ctx, hash)
	if errors.Is(err, session.ErrNotFound) {
		return nil, session.ErrInvalid
	}

	if err != nil {
		return nil, err
	}

	if !time.Now().Before(value.ExpiresAt) {
		return nil, session.ErrInvalid
	}

	return value, nil
}

func (s *SessionService) Logout(ctx context.Context, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	hash, ok := sessionTokenHash(token)
	if !ok {
		return nil
	}

	return s.repository.DeleteByTokenHash(ctx, hash)
}

func newSession(userID int64, now time.Time, lifetime time.Duration) (*session.Session, *session.Token) {
	raw := make([]byte, sessionTokenLength)
	rand.Read(raw)
	hash := sha256.Sum256(raw)
	expiresAt := now.Add(lifetime)

	value := &session.Session{
		TokenHash: hash[:],
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}

	token := &session.Token{
		Value:     base64.RawURLEncoding.EncodeToString(raw),
		ExpiresAt: expiresAt,
	}

	return value, token
}

func sessionTokenHash(token string) ([]byte, bool) {
	if len(token) != base64.RawURLEncoding.EncodedLen(sessionTokenLength) {
		return nil, false
	}

	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) != sessionTokenLength {
		return nil, false
	}

	hash := sha256.Sum256(raw)

	return hash[:], true
}
