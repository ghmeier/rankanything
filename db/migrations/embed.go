// Package migrations embeds the goose migration files and applies them.
//
// Embedding keeps the deployed artifact a single binary, and makes the schema
// the binary migrates to the same one its source was tested against.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed *.sql
var files embed.FS

// Up holds a session advisory lock throughout, so two instances booting at
// once during a rolling deploy serialize rather than racing.
func Up(ctx context.Context, db *sql.DB) error {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("migrations: build session locker: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, files, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("migrations: build provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrations: apply: %w", err)
	}
	return nil
}
