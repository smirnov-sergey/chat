package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"chat/internal/model/session"
	"chat/internal/model/user"
	"chat/internal/validation"
)

type UserRepository interface {
	GetByUsername(ctx context.Context, username string) (*user.User, error)
	GetByID(ctx context.Context, id int64) (*user.User, error)
	UpdatePhoto(ctx context.Context, id int64, photo *string, updatedAt time.Time) (*string, error)
}

type UserRegistrationStore interface {
	Create(ctx context.Context, u *user.User, s *session.Session) error
}

type PasswordService interface {
	Hash(password string) (string, error)
	Verify(password, encoded string) (bool, error)
}

type UserSessions interface {
	Create(ctx context.Context, userID int64) (*session.Token, error)
	Logout(ctx context.Context, token string) error
}

type PhotoProcessor interface {
	Process(ctx context.Context, data []byte) ([]byte, error)
}

type PhotoStorage interface {
	Save(data []byte) (string, error)
	Delete(filename string) error
}

type UserSession struct {
	User  *user.User
	Token *session.Token
}

type UserService struct {
	repository        UserRepository
	registrationStore UserRegistrationStore
	passwords         PasswordService
	sessions          UserSessions
	photoProcessor    PhotoProcessor
	photoStorage      PhotoStorage
	sessionLifetime   time.Duration
}

func NewUserService(
	repository UserRepository,
	registrationStore UserRegistrationStore,
	passwords PasswordService,
	sessions UserSessions,
	photoProcessor PhotoProcessor,
	photoStorage PhotoStorage,
	sessionLifetime time.Duration,
) (*UserService, error) {
	if sessionLifetime <= 0 {
		return nil, errors.New("session lifetime must be positive")
	}

	return &UserService{
		repository:        repository,
		registrationStore: registrationStore,
		passwords:         passwords,
		sessions:          sessions,
		photoProcessor:    photoProcessor,
		photoStorage:      photoStorage,
		sessionLifetime:   sessionLifetime,
	}, nil
}

func (s *UserService) Register(ctx context.Context, username, name, password string) (*UserSession, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if err := validation.ValidateUsername(username); err != nil {
		return nil, err
	}

	name = strings.TrimSpace(name)
	if err := validation.ValidateName(name); err != nil {
		return nil, err
	}

	if err := validation.ValidatePassword(password); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	passwordHash, err := s.passwords.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	u := &user.User{
		Username:     username,
		Name:         name,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	initialSession, token := newSession(0, now, s.sessionLifetime)
	if err := s.registrationStore.Create(ctx, u, initialSession); err != nil {
		return nil, err
	}

	return &UserSession{User: u, Token: token}, nil
}

func (s *UserService) Login(ctx context.Context, username, password string) (*UserSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	username = strings.ToLower(strings.TrimSpace(username))
	if err := validation.ValidateUsername(username); err != nil {
		return nil, user.ErrInvalidCredentials
	}

	if password == "" || !utf8.ValidString(password) || utf8.RuneCountInString(password) > 128 {
		return nil, user.ErrInvalidCredentials
	}

	u, err := s.repository.GetByUsername(ctx, username)
	if err != nil && !errors.Is(err, user.ErrNotFound) {
		return nil, fmt.Errorf("find user for login: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var matches bool
	if errors.Is(err, user.ErrNotFound) || u.PasswordHash == "" {
		_, err = s.passwords.Hash(password)
	} else {
		matches, err = s.passwords.Verify(password, u.PasswordHash)
	}

	if err != nil {
		return nil, fmt.Errorf("check password: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if !matches {
		return nil, user.ErrInvalidCredentials
	}

	token, err := s.sessions.Create(ctx, u.ID)
	if err != nil {
		return nil, fmt.Errorf("create login session: %w", err)
	}

	return &UserSession{User: u, Token: token}, nil
}

func (s *UserService) GetByID(ctx context.Context, id int64) (*user.User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	u, err := s.repository.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user by ID: %w", err)
	}

	return u, nil
}

func (s *UserService) Logout(ctx context.Context, token string) error {
	return s.sessions.Logout(ctx, token)
}

func (s *UserService) UploadPhoto(ctx context.Context, userID int64, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if _, err := s.repository.GetByID(ctx, userID); err != nil {
		return fmt.Errorf("find user for photo upload: %w", err)
	}

	processed, err := s.photoProcessor.Process(ctx, data)
	if err != nil {
		return fmt.Errorf("process user photo: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	filename, err := s.photoStorage.Save(processed)
	if err != nil {
		return fmt.Errorf("save user photo: %w", err)
	}

	if err := ctx.Err(); err != nil {
		s.deletePhotoFile(filename)
		return err
	}

	previous, err := s.repository.UpdatePhoto(ctx, userID, &filename, time.Now().UTC())
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			s.deletePhotoFile(filename)
		} else {
			// A connection failure can leave the update outcome uncertain.
			// Removing the new file could break an already committed reference.
			log.Printf("user photo update failed; retained file %q: %v", filename, err)
		}
		return fmt.Errorf("update user photo: %w", err)
	}

	if previous != nil && *previous != filename {
		s.deletePhotoFile(*previous)
	}

	return nil
}

func (s *UserService) deletePhotoFile(filename string) {
	if err := s.photoStorage.Delete(filename); err != nil {
		log.Printf("delete unused user photo %q: %v", filename, err)
	}
}
