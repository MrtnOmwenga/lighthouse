package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OwnerTenant returns the owner tenant's ID, creating it on first use.
func OwnerTenant(ctx context.Context, pool *pgxpool.Pool, name string) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `SELECT lighthouse_owner_tenant($1)`, name).Scan(&id)
	return id, err
}

// CreateSandbox creates a sandbox tenant, then runs seed inside it.
func CreateSandbox(ctx context.Context, pool *pgxpool.Pool, id string, seed func(pgx.Tx) error) error {
	return WithTenant(ctx, pool, id, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, kind, name) VALUES ($1, 'sandbox', 'Sandbox')`, id); err != nil {
			return err
		}
		return seed(tx)
	})
}

type Session struct {
	TenantID    string
	Role        string // owner or sandbox
	GitHubLogin *string
	ExpiresAt   time.Time
	Device      string // what kind of device started it: phone, tablet or desktop
}

// LookupSession finds the session for a cookie's token hash, in any tenant.
func LookupSession(ctx context.Context, pool *pgxpool.Pool, tokenHash string) (Session, error) {
	var s Session
	err := pool.QueryRow(ctx, `SELECT tenant_id, role, github_login, expires_at FROM lighthouse_session($1)`, tokenHash).
		Scan(&s.TenantID, &s.Role, &s.GitHubLogin, &s.ExpiresAt)
	return s, notFound(err)
}

func CreateSession(ctx context.Context, tx pgx.Tx, tokenHash string, s Session) error {
	_, err := tx.Exec(ctx, `INSERT INTO sessions (token_hash, tenant_id, role, github_login, expires_at, device) VALUES ($1, $2, $3, $4, $5, $6)`,
		tokenHash, s.TenantID, s.Role, s.GitHubLogin, s.ExpiresAt, s.Device)
	return err
}

// SessionInfo is a session as its owner may see it: never the token or its hash.
type SessionInfo struct {
	ID        string    `json:"id"`
	Login     *string   `json:"login"`
	Device    string    `json:"device"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Current   bool      `json:"current"` // the session asking
}

// ListSessions lists the tenant's live sessions, newest first, marking the one whose token hash
// is currentHash.
func ListSessions(ctx context.Context, tx pgx.Tx, currentHash string) ([]SessionInfo, error) {
	rows, err := tx.Query(ctx, `SELECT id, github_login, device, created_at, expires_at, token_hash = $1
		FROM sessions WHERE expires_at > now() ORDER BY created_at DESC`, currentHash)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SessionInfo, error) {
		var s SessionInfo
		return s, r.Scan(&s.ID, &s.Login, &s.Device, &s.CreatedAt, &s.ExpiresAt, &s.Current)
	})
}

// EndSession ends one of the tenant's sessions by id.
func EndSession(ctx context.Context, tx pgx.Tx, id string) error {
	tag, err := tx.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// EndOtherSessions ends every session of the tenant except the one whose token hash is keepHash,
// and reports how many.
func EndOtherSessions(ctx context.Context, tx pgx.Tx, keepHash string) (int64, error) {
	tag, err := tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash <> $1`, keepHash)
	return tag.RowsAffected(), err
}

// AuthEvent is one entry in the record of sign-ins.
type AuthEvent struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Kind     string    `json:"kind"` // signed_in, refused, signed_out, sessions_ended
	Login    string    `json:"login"`
	GitHubID *int64    `json:"githubId"`
	Detail   string    `json:"detail"`
}

func AddAuthEvent(ctx context.Context, tx pgx.Tx, tenantID string, e AuthEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO auth_events (tenant_id, kind, login, github_id, detail) VALUES ($1, $2, left($3, 100), $4, left($5, 200))`,
		tenantID, e.Kind, e.Login, e.GitHubID, e.Detail)
	return err
}

func RecentAuthEvents(ctx context.Context, tx pgx.Tx, limit int) ([]AuthEvent, error) {
	rows, err := tx.Query(ctx, `SELECT id, at, kind, login, github_id, detail FROM auth_events ORDER BY at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AuthEvent, error) {
		var e AuthEvent
		return e, r.Scan(&e.ID, &e.At, &e.Kind, &e.Login, &e.GitHubID, &e.Detail)
	})
}

// RateHit counts one more use of key in the current window and reports whether it is within
// limit. The count is in the database, so it survives restarts and is shared between instances.
func RateHit(ctx context.Context, pool *pgxpool.Pool, key string, window time.Duration, limit int) (bool, error) {
	var ok bool
	err := pool.QueryRow(ctx, `SELECT lighthouse_rate_hit($1, make_interval(secs => $2), $3)`, key, window.Seconds(), limit).Scan(&ok)
	return ok, err
}

// PruneSecurity deletes sign-in records older than keep, and finished rate-limit windows.
func PruneSecurity(ctx context.Context, pool *pgxpool.Pool, keep time.Duration) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT lighthouse_prune_security($1)`, keep).Scan(&n)
	return n, err
}

func DeleteSession(ctx context.Context, tx pgx.Tx, tokenHash string) error {
	_, err := tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

type Pruned struct{ Checks, Sandboxes, Sessions int64 }

// Prune deletes old checks, expired sessions and sandboxes older than keepSandboxes.
func Prune(ctx context.Context, pool *pgxpool.Pool, keepChecks, keepSandboxes time.Duration) (Pruned, error) {
	var p Pruned
	err := pool.QueryRow(ctx, `SELECT checks, sandboxes, sessions FROM lighthouse_prune($1, $2)`, keepChecks, keepSandboxes).
		Scan(&p.Checks, &p.Sandboxes, &p.Sessions)
	return p, err
}
