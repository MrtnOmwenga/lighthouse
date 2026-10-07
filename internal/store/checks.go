package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Check struct {
	ID           int64      `json:"id"`
	MonitorID    string     `json:"monitorId"`
	At           time.Time  `json:"at"`
	OK           bool       `json:"ok"`
	StatusCode   *int       `json:"statusCode"`
	LatencyMS    int        `json:"latencyMs"`
	Failure      *string    `json:"failure"`
	TLSExpiresAt *time.Time `json:"tlsExpiresAt"`
	// Warmup marks a passing check that woke a sleeping service: it counts for uptime and is left
	// out of response-time figures.
	Warmup bool `json:"warmup"`
}

func InsertCheck(ctx context.Context, tx pgx.Tx, tenantID string, c Check) (Check, error) {
	err := tx.QueryRow(ctx, `INSERT INTO checks (tenant_id, monitor_id, at, ok, status_code, latency_ms, failure, tls_expires_at, warmup)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		tenantID, c.MonitorID, c.At, c.OK, c.StatusCode, c.LatencyMS, c.Failure, c.TLSExpiresAt, c.Warmup).Scan(&c.ID)
	return c, err
}

// RecentChecks returns a monitor's checks, newest first, before the cursor (0 = from the newest).
func RecentChecks(ctx context.Context, tx pgx.Tx, monitorID string, before int64, limit int) ([]Check, error) {
	rows, err := tx.Query(ctx, `SELECT id, monitor_id, at, ok, status_code, latency_ms, failure, tls_expires_at, warmup FROM checks
		WHERE monitor_id = $1 AND ($2 = 0 OR id < $2) ORDER BY id DESC LIMIT $3`, monitorID, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Check, error) {
		var c Check
		return c, r.Scan(&c.ID, &c.MonitorID, &c.At, &c.OK, &c.StatusCode, &c.LatencyMS, &c.Failure, &c.TLSExpiresAt, &c.Warmup)
	})
}
