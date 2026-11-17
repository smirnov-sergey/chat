package storage_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"chat/internal/storage"
)

func TestPhotoStorageSaveReadDelete(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "data", "photos")
	photos, err := storage.NewPhotoStorage(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { photos.Close() })
	data := []byte("prepared image bytes")
	first, err := photos.Save(data)
	if err != nil {
		t.Fatal(err)
	}
	second, err := photos.Save(data)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || second == "" || first == second || filepath.Base(first) != first || filepath.Base(second) != second {
		t.Fatal("each saved photo must receive a distinct generated filename without directories")
	}
	if got, err := photos.Read(first); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read saved photo: data=%q, error=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(directory, first)); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("photo must be stored in the configured directory: data=%q, error=%v", got, err)
	}
	for range 2 {
		if err := photos.Delete(first); err != nil {
			t.Fatalf("deletion must be idempotent: %v", err)
		}
	}
	if _, err := photos.Read(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read deleted photo error = %v, want os.ErrNotExist", err)
	}
	if got, err := photos.Read(second); err != nil || !bytes.Equal(got, data) {
		t.Fatal("deleting one photo must preserve the other")
	}
	if err := photos.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.NewPhotoStorage(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	if got, err := reopened.Read(second); err != nil || !bytes.Equal(got, data) {
		t.Fatal("saved photos must remain readable after reopening the storage")
	}
}

func TestPhotoStorageRejectsOutsidePaths(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside.png")
	const content = "outside file must remain unchanged"
	if err := os.WriteFile(outside, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	photos, err := storage.NewPhotoStorage(filepath.Join(parent, "photos"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { photos.Close() })
	for _, filename := range []string{"../outside.png", outside} {
		t.Run(filename, func(t *testing.T) {
			if _, err := photos.Read(filename); err == nil {
				t.Fatal("reading outside the photo directory must fail")
			}
			if err := photos.Delete(filename); err == nil {
				t.Fatal("deleting outside the photo directory must fail")
			}
		})
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != content {
		t.Fatal("outside file must remain unchanged")
	}
}

func TestPhotoStorageRejectsOutsideDirectoryLink(t *testing.T) {
	parent := t.TempDir()
	outsideDirectory := filepath.Join(parent, "outside")
	if err := os.Mkdir(outsideDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(outsideDirectory, "photo.png")
	const content = "outside file must remain unchanged"
	if err := os.WriteFile(outside, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(parent, "photos")
	photos, err := storage.NewPhotoStorage(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { photos.Close() })
	link := filepath.Join(directory, "link")
	if runtime.GOOS == "windows" {
		// Directory junctions do not require the Windows symlink privilege.
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
			"New-Item -ItemType Junction -Path $env:CHAT_TEST_LINK -Target $env:CHAT_TEST_TARGET -ErrorAction Stop | Out-Null")
		command.Env = append(command.Environ(), "CHAT_TEST_LINK="+link, "CHAT_TEST_TARGET="+outsideDirectory)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("create directory junction: %v\n%s", err, output)
		}
	} else if err := os.Symlink(filepath.Join("..", "outside"), link); err != nil {
		t.Fatalf("create directory symlink: %v", err)
	}
	filename := filepath.Join("link", "photo.png")
	if got, err := os.ReadFile(filepath.Join(directory, filename)); err != nil || string(got) != content {
		t.Fatalf("directory link must lead to the existing outside file: %v", err)
	}
	if _, err := photos.Read(filename); err == nil {
		t.Fatal("a directory link must not allow reading outside the photo directory")
	}
	if err := photos.Delete(filename); err == nil {
		t.Fatal("a directory link must not allow deleting outside the photo directory")
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != content {
		t.Fatal("outside file must remain unchanged")
	}
}

func TestNewPhotoStorageRejectsInvalidDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"", file, filepath.Join(file, "photos")} {
		if photos, err := storage.NewPhotoStorage(directory); err == nil || photos != nil {
			if photos != nil {
				photos.Close()
			}
			t.Fatalf("directory %q must fail without returning storage", directory)
		}
	}
}

func TestPhotoStorageClosed(t *testing.T) {
	photos, err := storage.NewPhotoStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	filename, err := photos.Save([]byte("photo"))
	if err != nil {
		photos.Close()
		t.Fatal(err)
	}
	if err := photos.Close(); err != nil {
		t.Fatal(err)
	}
	if filename, err := photos.Save([]byte("another photo")); err == nil || filename != "" {
		t.Fatal("closed storage must not save files or return a filename")
	}
	if _, err := photos.Read(filename); err == nil {
		t.Fatal("closed storage must not read files")
	}
	if err := photos.Delete(filename); err == nil {
		t.Fatal("closed storage must not delete files")
	}
}
