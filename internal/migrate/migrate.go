package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// Up applies pending SQL migrations and on success returns the number applied.
// The caller owns db and must close it after Up returns.
func Up(ctx context.Context, db *sql.DB, files fs.FS) (int, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return 0, fmt.Errorf("read migrations: %w", err)
	}
	// Goose ignores files with invalid numeric prefixes; report SQL typos instead.
	for _, entry := range entries {
		if !entry.IsDir() && path.Ext(entry.Name()) == ".sql" {
			if _, err := goose.NumericComponent(entry.Name()); err != nil {
				return 0, fmt.Errorf("invalid migration filename %q: %w", entry.Name(), err)
			}
		}
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return 0, fmt.Errorf("create migration lock: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files,
		goose.WithDisableGlobalRegistry(true),
		goose.WithSessionLocker(locker),
	)
	if errors.Is(err, goose.ErrNoMigrations) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("prepare migrations: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("apply migrations: %w", err)
	}
	return len(results), nil
}
