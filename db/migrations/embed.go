// Package migrations embeds the goose migration files and applies them.
//
// Embedding rather than reading db/migrations off disk is what lets the
// deployed artifact stay a single binary: the container image carries no
// SQL files, and the schema the binary migrates to is by construction the
// one its own source was tested against. It also removes the need to
// resolve a path relative to the source tree, which was fragile across git
// worktrees.
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

// Up applies every pending migration to db.
//
// A session-level advisory lock is held for the duration, so two instances
// booting at once — a rolling deploy running the new container before the
// old one exits — serialize rather than racing to apply the same migration.
// Whichever loses the race finds nothing left to do.
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
