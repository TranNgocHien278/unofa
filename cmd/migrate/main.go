// Command migrate applies the numbered SQL migrations in migrations/ to the
// database in DATABASE_URL. Migrations are one-way and numbered 0001_, 0002_, ...
//
// Applied versions are recorded in schema_migrations so a second run is a no-op.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/unofa/unofa/migrations"
)

// migrateLockID is a fixed Postgres session advisory lock key ("UNOFAMIG")
// so concurrent migrate processes serialize instead of racing on DDL.
const migrateLockID int64 = 0x554e4f46414d4947

func main() {
	dir := flag.String("dir", ".", "directory inside the embedded FS holding *.sql")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	if err := run(ctx, conn, *dir); err != nil {
		log.Fatalf("migrate: %v", err)
	}
}

func run(ctx context.Context, conn *pgx.Conn, dir string) error {
	return runWithFS(ctx, conn, migrations.FS, dir)
}

func runWithFS(ctx context.Context, conn *pgx.Conn, fsys fs.FS, dir string) error {
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockID); err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrateLockID)
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}

	names, err := selectMigrations(fsys, dir)
	if err != nil {
		return err
	}

	for _, name := range names {
		// Check if the migration has already been applied
		var exists bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}

		// Read the migration file following abs path
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return err
		}

		// Start transaction to exec migrate
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("%s: %w", name, err)
		}

		// Record the applied migration in schema_migrations
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}

		// Commit the transaction
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		log.Printf("applied %s", name)
	}
	return nil
}

// selectMigrations returns sorted up-migration filenames under dir, excluding
// directories, non-SQL files, and down migrations (*_down.sql, *.down.sql).
func selectMigrations(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !isUpMigration(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

func isUpMigration(name string) bool {
	lower := strings.ToLower(name)
	if !strings.HasSuffix(lower, ".sql") {
		return false
	}
	return !strings.HasSuffix(lower, "_down.sql") && !strings.HasSuffix(lower, ".down.sql")
}
