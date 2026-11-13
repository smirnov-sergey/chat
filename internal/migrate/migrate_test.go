package migrate_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"chat/internal/migrate"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestUpEmptyDirectory(t *testing.T) {
	db := unopenedDB(t)
	count, err := migrate.Up(context.Background(), db, fstest.MapFS{})
	if err != nil || count != 0 {
		t.Fatalf("Up = (%d, %v), want (0, nil)", count, err)
	}
}

func TestUpMissingDirectory(t *testing.T) {
	db := unopenedDB(t)
	_, err := migrate.Up(context.Background(), db, os.DirFS(t.TempDir()+"/missing"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Up error = %v, want missing directory error", err)
	}
}

func TestUpRejectsDuplicateVersions(t *testing.T) {
	db := unopenedDB(t)
	files := fstest.MapFS{
		"00001_first.sql":  {Data: []byte("-- +goose Up\nSELECT 1;\n")},
		"00001_second.sql": {Data: []byte("-- +goose Up\nSELECT 2;\n")},
	}
	if _, err := migrate.Up(context.Background(), db, files); err == nil {
		t.Fatal("expected duplicate version error")
	}
}

func TestUpRejectsInvalidSQLFilename(t *testing.T) {
	files := fstest.MapFS{
		"create_users.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")},
	}
	if _, err := migrate.Up(context.Background(), unopenedDB(t), files); err == nil {
		t.Fatal("expected invalid filename error")
	}
}

func TestUpHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	files := fstest.MapFS{
		"00001_first.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")},
	}
	_, err := migrate.Up(ctx, unopenedDB(t), files)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Up error = %v, want context canceled", err)
	}
}

func unopenedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", "postgres://unused:unused@127.0.0.1:1/unused?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
