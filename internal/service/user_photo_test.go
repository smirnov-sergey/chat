package service_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"chat/internal/model/user"
	"chat/internal/photo"
	"chat/internal/service"
	"chat/internal/storage"
)

type fakePhotoProcessor struct {
	process func(context.Context, []byte) ([]byte, error)
}

func (fake fakePhotoProcessor) Process(ctx context.Context, data []byte) ([]byte, error) {
	return fake.process(ctx, data)
}

type fakePhotoStorage struct {
	save   func([]byte) (string, error)
	delete func(string) error
}

func (fake fakePhotoStorage) Save(data []byte) (string, error) { return fake.save(data) }
func (fake fakePhotoStorage) Delete(filename string) error     { return fake.delete(filename) }

var (
	_ service.PhotoProcessor = fakePhotoProcessor{}
	_ service.PhotoProcessor = (*photo.Processor)(nil)
	_ service.PhotoStorage   = fakePhotoStorage{}
	_ service.PhotoStorage   = (*storage.PhotoStorage)(nil)
)

func newPhotoUserService(t *testing.T, repo fakeUserRepository, processor fakePhotoProcessor, files fakePhotoStorage) *service.UserService {
	t.Helper()
	s, err := service.NewUserService(repo, nil, nil, nil, processor, files, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUserServiceUploadPhoto(t *testing.T) {
	old := "old-photo"
	stale := "stale-photo"
	for _, tt := range []struct {
		name     string
		current  *string
		previous *string
	}{
		{"first upload", nil, nil},
		{"replacement", &old, &old},
		{"concurrent replacement uses actual previous photo", &stale, &old},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			input, processed := []byte("uploaded JPEG"), []byte("processed PNG")
			var calls []string
			before := time.Now().UTC()
			repo := fakeUserRepository{
				getByID: func(gotCtx context.Context, id int64) (*user.User, error) {
					calls = append(calls, "get")
					if gotCtx != ctx || id != 42 {
						t.Fatal("lookup must receive the caller's context and user ID")
					}
					return &user.User{ID: id, Photo: tt.current}, nil
				},
				updatePhoto: func(gotCtx context.Context, id int64, value *string, updatedAt time.Time) (*string, error) {
					calls = append(calls, "update")
					if gotCtx != ctx || id != 42 || value == nil || *value != "new-photo" {
						t.Fatal("update must receive the saved filename and original context and user ID")
					}
					if updatedAt.Location() != time.UTC || updatedAt.Before(before) || updatedAt.After(time.Now().UTC()) {
						t.Fatal("photo update time must be the current time in UTC")
					}
					return tt.previous, nil
				},
			}
			processor := fakePhotoProcessor{process: func(gotCtx context.Context, data []byte) ([]byte, error) {
				calls = append(calls, "process")
				if gotCtx != ctx || !bytes.Equal(data, input) {
					t.Fatal("processor must receive the original image bytes and context")
				}
				return processed, nil
			}}
			files := fakePhotoStorage{
				save: func(data []byte) (string, error) {
					calls = append(calls, "save")
					if !bytes.Equal(data, processed) {
						t.Fatal("only the processed image must be saved")
					}
					return "new-photo", nil
				},
				delete: func(filename string) error {
					calls = append(calls, "delete")
					if filename != old {
						t.Fatal("only the previous photo must be deleted after the update")
					}
					return nil
				},
			}
			if err := newPhotoUserService(t, repo, processor, files).UploadPhoto(ctx, 42, input); err != nil {
				t.Fatal(err)
			}
			want := []string{"get", "process", "save", "update"}
			if tt.previous != nil {
				want = append(want, "delete")
			}
			if !slices.Equal(calls, want) {
				t.Fatalf("calls=%v, want %v", calls, want)
			}
		})
	}
}

func TestUserServiceUploadPhotoFailures(t *testing.T) {
	failure := errors.New("operation failed")
	for _, stage := range []string{"lookup", "processing", "save", "update", "user removed", "old file deletion"} {
		t.Run(stage, func(t *testing.T) {
			var calls []string
			old := "old-photo"
			repo := fakeUserRepository{
				getByID: func(context.Context, int64) (*user.User, error) {
					calls = append(calls, "get")
					if stage == "lookup" {
						return nil, failure
					}
					return &user.User{ID: 42, Photo: &old}, nil
				},
				updatePhoto: func(context.Context, int64, *string, time.Time) (*string, error) {
					calls = append(calls, "update")
					if stage == "update" {
						return nil, failure
					}
					if stage == "user removed" {
						return nil, fmt.Errorf("update: %w", user.ErrNotFound)
					}
					return &old, nil
				},
			}
			processor := fakePhotoProcessor{process: func(context.Context, []byte) ([]byte, error) {
				calls = append(calls, "process")
				if stage == "processing" {
					return nil, failure
				}
				return []byte("PNG"), nil
			}}
			files := fakePhotoStorage{
				save: func([]byte) (string, error) {
					calls = append(calls, "save")
					if stage == "save" {
						return "", failure
					}
					return "new-photo", nil
				},
				delete: func(filename string) error {
					calls = append(calls, "delete "+filename)
					if stage == "old file deletion" {
						return failure
					}
					return nil
				},
			}
			err := newPhotoUserService(t, repo, processor, files).UploadPhoto(t.Context(), 42, []byte("JPEG"))
			wantCalls := []string{"get"}
			wantError := failure
			switch stage {
			case "processing":
				wantCalls = append(wantCalls, "process")
			case "save":
				wantCalls = append(wantCalls, "process", "save")
			case "update":
				wantCalls = append(wantCalls, "process", "save", "update")
			case "user removed":
				wantCalls = append(wantCalls, "process", "save", "update", "delete new-photo")
				wantError = user.ErrNotFound
			case "old file deletion":
				wantCalls = append(wantCalls, "process", "save", "update", "delete old-photo")
				wantError = nil
			}
			if (wantError == nil && err != nil) || (wantError != nil && !errors.Is(err, wantError)) {
				t.Fatalf("Upload error=%v, want %v", err, wantError)
			}
			if !slices.Equal(calls, wantCalls) {
				t.Fatalf("calls=%v, want %v", calls, wantCalls)
			}
		})
	}
}

func TestUserServiceUploadPhotoCancellation(t *testing.T) {
	for _, stage := range []string{"before lookup", "after processing", "after save", "after update"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if stage == "before lookup" {
				cancel()
			}
			var calls []string
			repo := fakeUserRepository{
				getByID: func(context.Context, int64) (*user.User, error) {
					calls = append(calls, "get")
					return &user.User{ID: 42}, nil
				},
				updatePhoto: func(context.Context, int64, *string, time.Time) (*string, error) {
					calls = append(calls, "update")
					if stage == "after update" {
						cancel()
					}
					return nil, nil
				},
			}
			processor := fakePhotoProcessor{process: func(context.Context, []byte) ([]byte, error) {
				calls = append(calls, "process")
				if stage == "after processing" {
					cancel()
				}
				return []byte("PNG"), nil
			}}
			files := fakePhotoStorage{
				save: func([]byte) (string, error) {
					calls = append(calls, "save")
					if stage == "after save" {
						cancel()
					}
					return "new-photo", nil
				},
				delete: func(filename string) error {
					calls = append(calls, "delete "+filename)
					return nil
				},
			}
			err := newPhotoUserService(t, repo, processor, files).UploadPhoto(ctx, 42, nil)
			var wantCalls []string
			switch stage {
			case "after processing":
				wantCalls = []string{"get", "process"}
			case "after save":
				wantCalls = []string{"get", "process", "save", "delete new-photo"}
			case "after update":
				wantCalls = []string{"get", "process", "save", "update"}
			}
			if stage == "after update" {
				if err != nil {
					t.Fatalf("committed upload must remain successful after cancellation: %v", err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("Upload error=%v, want context.Canceled", err)
			}
			if !slices.Equal(calls, wantCalls) {
				t.Fatalf("calls=%v, want %v", calls, wantCalls)
			}
		})
	}
}

func TestUserServiceDeletePhoto(t *testing.T) {
	filename := "previous-photo"
	for _, tt := range []struct {
		name              string
		previous          *string
		deleteError       error
		cancelAfterUpdate bool
	}{
		{"existing photo", &filename, nil, false},
		{"no photo", nil, nil, false},
		{"file cleanup failure does not undo deletion", &filename, errors.New("disk unavailable"), false},
		{"cancellation after update does not prevent cleanup", &filename, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			before := time.Now().UTC()
			var calls []string
			repo := fakeUserRepository{updatePhoto: func(gotCtx context.Context, id int64, value *string, updatedAt time.Time) (*string, error) {
				calls = append(calls, "update")
				if gotCtx != ctx || id != 42 || value != nil {
					t.Fatal("deletion must update the supplied user with NULL using the caller's context")
				}
				if updatedAt.Location() != time.UTC || updatedAt.Before(before) || updatedAt.After(time.Now().UTC()) {
					t.Fatal("modification time must be the current time in UTC")
				}
				if tt.cancelAfterUpdate {
					cancel()
				}
				return tt.previous, nil
			}}
			files := fakePhotoStorage{delete: func(got string) error {
				calls = append(calls, "delete")
				if got != filename {
					t.Fatalf("deleted filename = %q, want %q", got, filename)
				}
				return tt.deleteError
			}}
			s := newPhotoUserService(t, repo, fakePhotoProcessor{}, files)
			if err := s.DeletePhoto(ctx, 42); err != nil {
				t.Fatalf("committed deletion must be successful: %v", err)
			}
			want := []string{"update"}
			if tt.previous != nil {
				want = append(want, "delete")
			}
			if !slices.Equal(calls, want) {
				t.Fatalf("calls = %v, want %v", calls, want)
			}
		})
	}
}

func TestUserServiceDeletePhotoRepositoryErrors(t *testing.T) {
	for _, want := range []error{user.ErrNotFound, errors.New("database unavailable"), context.Canceled} {
		t.Run(want.Error(), func(t *testing.T) {
			filename := "previous-photo"
			repo := fakeUserRepository{updatePhoto: func(context.Context, int64, *string, time.Time) (*string, error) {
				return &filename, fmt.Errorf("update: %w", want)
			}}
			s := newPhotoUserService(t, repo, fakePhotoProcessor{}, fakePhotoStorage{})
			if err := s.DeletePhoto(t.Context(), 42); !errors.Is(err, want) {
				t.Fatalf("DeletePhoto error = %v, want %v", err, want)
			}
		})
	}
}

func TestUserServiceDeletePhotoCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := newPhotoUserService(t, fakeUserRepository{}, fakePhotoProcessor{}, fakePhotoStorage{})
	if err := s.DeletePhoto(ctx, 42); !errors.Is(err, context.Canceled) {
		t.Fatalf("DeletePhoto error = %v, want context.Canceled without calling dependencies", err)
	}
}
