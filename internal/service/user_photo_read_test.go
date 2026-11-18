package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"testing"

	"chat/internal/model/user"
)

func TestUserServiceGetPhoto(t *testing.T) {
	ctx := t.Context()
	filename := "stored-photo"
	want := []byte("prepared PNG")
	var calls []string
	repo := fakeUserRepository{getByID: func(gotCtx context.Context, id int64) (*user.User, error) {
		calls = append(calls, "get")
		if gotCtx != ctx || id != 42 {
			t.Fatal("lookup must receive the caller's context and user ID")
		}
		return &user.User{ID: id, Photo: &filename}, nil
	}}
	files := fakePhotoStorage{read: func(got string) ([]byte, error) {
		calls = append(calls, "read")
		if got != filename {
			t.Fatalf("read filename=%q, want %q", got, filename)
		}
		return want, nil
	}}
	s := newPhotoUserService(t, repo, fakePhotoProcessor{}, files)
	got, err := s.GetPhoto(ctx, 42)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("GetPhoto data=%q, error=%v; want stored bytes and nil", got, err)
	}
	if !slices.Equal(calls, []string{"get", "read"}) {
		t.Fatalf("calls=%v, want only lookup and read", calls)
	}
}

func TestUserServiceGetPhotoRepositoryErrors(t *testing.T) {
	for _, want := range []error{user.ErrNotFound, errors.New("database unavailable"), context.Canceled} {
		t.Run(want.Error(), func(t *testing.T) {
			repo := fakeUserRepository{getByID: func(context.Context, int64) (*user.User, error) {
				return nil, fmt.Errorf("lookup: %w", want)
			}}
			s := newPhotoUserService(t, repo, fakePhotoProcessor{}, fakePhotoStorage{})
			if data, err := s.GetPhoto(t.Context(), 42); data != nil || !errors.Is(err, want) {
				t.Fatalf("GetPhoto error=%v, want %v and no file access", err, want)
			}
		})
	}
}

func TestUserServiceGetPhotoMissingAndUnreadable(t *testing.T) {
	filename := "stored-photo"
	for _, tt := range []struct {
		name    string
		photo   *string
		readErr error
		wantErr error
		lookups int
		reads   int
	}{
		{"no photo", nil, nil, user.ErrPhotoNotFound, 1, 0},
		{"missing file retries once", &filename, fs.ErrNotExist, user.ErrPhotoNotFound, 2, 2},
		{"permission failure is not retried", &filename, fs.ErrPermission, fs.ErrPermission, 1, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lookups, reads := 0, 0
			repo := fakeUserRepository{getByID: func(context.Context, int64) (*user.User, error) {
				lookups++
				return &user.User{ID: 42, Photo: tt.photo}, nil
			}}
			files := fakePhotoStorage{read: func(string) ([]byte, error) {
				reads++
				return nil, fmt.Errorf("storage: %w", tt.readErr)
			}}
			s := newPhotoUserService(t, repo, fakePhotoProcessor{}, files)
			data, err := s.GetPhoto(t.Context(), 42)
			if data != nil || !errors.Is(err, tt.wantErr) || lookups != tt.lookups || reads != tt.reads {
				t.Fatalf("error=%v, lookups=%d, reads=%d; want %v, %d, %d", err, lookups, reads, tt.wantErr, tt.lookups, tt.reads)
			}
		})
	}
}

func TestUserServiceGetPhotoDuringReplacementOrDeletion(t *testing.T) {
	old, replacement := "old-photo", "new-photo"
	for _, current := range []*string{&replacement, nil} {
		name := "replacement"
		if current == nil {
			name = "deletion"
		}
		t.Run(name, func(t *testing.T) {
			lookups, reads := 0, 0
			repo := fakeUserRepository{getByID: func(context.Context, int64) (*user.User, error) {
				lookups++
				if lookups == 1 {
					return &user.User{ID: 42, Photo: &old}, nil
				}
				return &user.User{ID: 42, Photo: current}, nil
			}}
			want := []byte("replacement PNG")
			files := fakePhotoStorage{read: func(filename string) ([]byte, error) {
				reads++
				if filename == old {
					return nil, fs.ErrNotExist
				}
				if filename != replacement {
					t.Fatalf("unexpected read: %q", filename)
				}
				return want, nil
			}}
			s := newPhotoUserService(t, repo, fakePhotoProcessor{}, files)
			data, err := s.GetPhoto(t.Context(), 42)
			if current == nil {
				if data != nil || !errors.Is(err, user.ErrPhotoNotFound) || reads != 1 {
					t.Fatal("deleted photo must return ErrPhotoNotFound without another file read")
				}
			} else if err != nil || !bytes.Equal(data, want) || reads != 2 {
				t.Fatalf("replacement read: data=%q, error=%v, reads=%d", data, err, reads)
			}
			if lookups != 2 {
				t.Fatalf("lookups=%d, want one refresh of the current photo reference", lookups)
			}
		})
	}
}

func TestUserServiceGetPhotoCancellation(t *testing.T) {
	for _, stage := range []string{"before lookup", "after lookup"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if stage == "before lookup" {
				cancel()
			}
			lookups := 0
			repo := fakeUserRepository{getByID: func(context.Context, int64) (*user.User, error) {
				lookups++
				cancel()
				filename := "photo"
				return &user.User{ID: 42, Photo: &filename}, nil
			}}
			s := newPhotoUserService(t, repo, fakePhotoProcessor{}, fakePhotoStorage{})
			if data, err := s.GetPhoto(ctx, 42); data != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled read: error=%v, want context.Canceled without accessing storage", err)
			}
			if stage == "before lookup" && lookups != 0 {
				t.Fatal("canceled request must not call the repository")
			}
		})
	}
}
