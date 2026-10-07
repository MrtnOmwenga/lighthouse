// Package store is Lighthouse's PostgreSQL layer: the connection pool, tenant-scoped transactions,
// migrations, and the queries the rest of the service uses.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver, for goose
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrNotFound means the row doesn't exist, or belongs to another tenant: row-level security makes
// the two indistinguishable, which is the point.
var ErrNotFound = errors.New("not found")

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// WithTenant runs fn in a transaction whose row-level security is scoped to tenantID. The setting
// is transaction-local, so it can't leak to the next use of the connection.
func WithTenant(ctx context.Context, pool *pgxpool.Pool, tenantID string, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// MigrateTo moves the schema to a version: up to the latest when version is negative, otherwise
// up or down to exactly that one (0 undoes everything). Going down is for undoing a migration by
// hand, and for testing that every migration can be undone.
func MigrateTo(ctx context.Context, db *sql.DB, version int64) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	var err error
	if version < 0 {
		err = goose.UpContext(ctx, db, "migrations")
	} else if current, verr := goose.GetDBVersionContext(ctx, db); verr != nil {
		err = verr
	} else if version < current {
		err = goose.DownToContext(ctx, db, "migrations", version)
	} else {
		err = goose.UpToContext(ctx, db, "migrations", version)
	}
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	return nil
}

// Migrate applies the migrations as the database owner (ownerURL), then makes sure the API's login
// role (the user in appURL, with appPassword) exists and is a member of lighthouse_app.
func Migrate(ctx context.Context, ownerURL, appURL, appPassword string) error {
	db, err := sql.Open("pgx", ownerURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := MigrateTo(ctx, db, -1); err != nil {
		return err
	}
	if appURL == "" {
		return nil
	}
	u, err := url.Parse(appURL)
	if err != nil {
		return fmt.Errorf("DATABASE_URL: %w", err)
	}
	password, ok := u.User.Password()
	if !ok || password == "" {
		password = appPassword
	}
	if password == "" {
		return errors.New("the API role needs a password: put it in DATABASE_URL or APP_DB_PASSWORD")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// format() quotes the role name and password; they arrive as parameters, never spliced in.
	if _, err := tx.ExecContext(ctx, "SELECT set_config('lighthouse.login', $1, true), set_config('lighthouse.password', $2, true)", u.User.Username(), password); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DO $$ BEGIN
		IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = current_setting('lighthouse.login')) THEN
			EXECUTE format('CREATE ROLE %I LOGIN PASSWORD %L', current_setting('lighthouse.login'), current_setting('lighthouse.password'));
		ELSE
			EXECUTE format('ALTER ROLE %I LOGIN PASSWORD %L', current_setting('lighthouse.login'), current_setting('lighthouse.password'));
		END IF;
		EXECUTE format('GRANT lighthouse_app TO %I', current_setting('lighthouse.login'));
	END $$`); err != nil {
		return err
	}
	return tx.Commit()
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
