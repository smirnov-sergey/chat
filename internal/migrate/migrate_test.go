package migrate_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"chat/internal/migrate"
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

func TestUpIntegration(t *testing.T) {
	t.Run("concurrent runners", func(t *testing.T) {
		db := integrationDB(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		files := fstest.MapFS{
			"00001_entries.sql": {Data: []byte("-- +goose Up\nCREATE TABLE entries (id integer PRIMARY KEY);\nINSERT INTO entries VALUES (1);\nSELECT pg_sleep(0.2);\n")},
		}
		type result struct {
			count int
			err   error
		}
		start := make(chan struct{})
		results := make(chan result, 2)
		for range 2 {
			go func() {
				<-start
				count, err := migrate.Up(ctx, db, files)
				results <- result{count, err}
			}()
		}
		close(start)
		total := 0
		for range 2 {
			got := <-results
			if got.err != nil {
				t.Errorf("concurrent Up: %v", got.err)
			}
			total += got.count
		}
		if total != 1 {
			t.Fatalf("total applied = %d, want 1", total)
		}
	})

	t.Run("apply and repeat", func(t *testing.T) {
		db := integrationDB(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		files := fstest.MapFS{
			"00001_entries.sql": {Data: []byte("-- +goose Up\nCREATE TABLE entries (id integer PRIMARY KEY);\nINSERT INTO entries VALUES (1);\n")},
		}
		count, err := migrate.Up(ctx, db, files)
		if err != nil || count != 1 {
			t.Fatalf("first Up = (%d, %v), want (1, nil)", count, err)
		}
		count, err = migrate.Up(ctx, db, files)
		if err != nil || count != 0 {
			t.Fatalf("second Up = (%d, %v), want (0, nil)", count, err)
		}
		var rows int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM entries").Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 1 {
			t.Fatalf("entries = %d, want 1", rows)
		}
	})

	t.Run("failed migration rolls back and can be retried", func(t *testing.T) {
		db := integrationDB(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		files := fstest.MapFS{
			"00001_entries.sql": {Data: []byte("-- +goose Up\nCREATE TABLE entries (id integer PRIMARY KEY);\n")},
			"00002_insert.sql":  {Data: []byte("-- +goose Up\nINSERT INTO entries VALUES (1);\nSELECT * FROM missing_table;\n")},
		}
		if _, err := migrate.Up(ctx, db, files); err == nil {
			t.Fatal("expected SQL error")
		}
		var rows int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM entries").Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Fatalf("entries = %d, want 0 after rollback", rows)
		}
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM goose_db_version WHERE version_id = 2 AND is_applied").Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Fatal("failed migration was recorded as applied")
		}
		files["00002_insert.sql"] = &fstest.MapFile{Data: []byte("-- +goose Up\nINSERT INTO entries VALUES (1);\n")}
		count, err := migrate.Up(ctx, db, files)
		if err != nil || count != 1 {
			t.Fatalf("retry Up = (%d, %v), want (1, nil)", count, err)
		}
	})
}
