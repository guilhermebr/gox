package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Migrate applies every pending up migration in fsys to the database at
// url. It is what Enable runs at boot and what a deploy script or a test
// bootstrap calls on its own.
//
// It opens one dedicated connection and closes it when done. golang-migrate
// holds an advisory lock on the connection it is given for as long as that
// connection lives; running it over the shared pool starved a service under
// a rolling deploy.
func Migrate(ctx context.Context, url string, fsys fs.FS, opts ...MigrateOption) error {
	var o migrateOptions
	for _, opt := range opts {
		opt(&o)
	}
	if fsys == nil {
		return errors.New("postgres: Migrate: nil filesystem")
	}
	root, err := migrationRoot(fsys)
	if err != nil {
		return err
	}
	src, err := iofs.New(fsys, root)
	if err != nil {
		return fmt.Errorf("postgres: migrations: %w", err)
	}

	cc, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("postgres: migrate: parse url: %w", err)
	}
	db := sql.OpenDB(stdlib.GetConnector(*cc))
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("postgres: migrate: connect: %w", err)
	}
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{MigrationsTable: o.table})
	if err != nil {
		return fmt.Errorf("postgres: migrate: driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", driver)
	if err != nil {
		return fmt.Errorf("postgres: migrate: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("postgres: migrate: %w", err)
	}
	return nil
}

// MigrateOption configures Migrate.
type MigrateOption func(*migrateOptions)

type migrateOptions struct {
	table string
}

// WithMigrationsTable stores the applied versions in another table than
// schema_migrations, for a database where another framework already owns
// that name. Enable reads the same setting from POSTGRES_MIGRATIONS_TABLE.
func WithMigrationsTable(name string) MigrateOption {
	return func(o *migrateOptions) { o.table = name }
}

// migrationRoot finds the directory holding the .sql files: the root of
// fsys, or its single subdirectory (embed.FS keeps "migrations/" as a
// prefix), so both `//go:embed migrations/*.sql` and a fs.Sub work.
func migrationRoot(fsys fs.FS) (string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return "", fmt.Errorf("postgres: migrations: %w", err)
	}
	dir := "."
	for {
		var dirs []fs.DirEntry
		hasSQL := false
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, e)
			} else if len(e.Name()) > 4 && e.Name()[len(e.Name())-4:] == ".sql" {
				hasSQL = true
			}
		}
		if hasSQL {
			return dir, nil
		}
		if len(dirs) != 1 {
			return "", fmt.Errorf("postgres: migrations: no .sql files found under %q", dir)
		}
		if dir == "." {
			dir = dirs[0].Name()
		} else {
			dir += "/" + dirs[0].Name()
		}
		if entries, err = fs.ReadDir(fsys, dir); err != nil {
			return "", fmt.Errorf("postgres: migrations: %w", err)
		}
	}
}
