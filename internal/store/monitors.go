package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Monitor struct {
	ID                  string     `json:"id"`
	TenantID            string     `json:"-"`
	Name                string     `json:"name"`
	Slug                string     `json:"slug"`
	Kind                string     `json:"kind"` // http or simulated
	URL                 *string    `json:"url"`
	SimulatedMode       string     `json:"simulatedMode"`
	IntervalSeconds     int        `json:"intervalSeconds"`
	TimeoutMS           int        `json:"timeoutMs"`
	ExpectedStatusMin   int        `json:"expectedStatusMin"`
	ExpectedStatusMax   int        `json:"expectedStatusMax"`
	ExpectedText        *string    `json:"expectedText"`
	FailureThreshold    int        `json:"failureThreshold"`
	RecoveryThreshold   int        `json:"recoveryThreshold"`
	Public              bool       `json:"public"`
	Paused              bool       `json:"paused"`
	AllowPrivateNetwork bool       `json:"allowPrivateNetwork"`
	Health              string     `json:"health"`
	ConsecutiveFailures int        `json:"consecutiveFailures"`
	ConsecutiveSuccess  int        `json:"consecutiveSuccesses"`
	OpenIncidentID      *string    `json:"openIncidentId"`
	LastCheckedAt       *time.Time `json:"lastCheckedAt"`
	NextCheckAt         time.Time  `json:"nextCheckAt"`
	CreatedAt           time.Time  `json:"createdAt"`
}

const monitorColumns = `id, tenant_id, name, slug, kind, url, simulated_mode, interval_seconds, timeout_ms,
	expected_status_min, expected_status_max, expected_text, failure_threshold, recovery_threshold, is_public,
	paused, allow_private_network, health, consecutive_failures, consecutive_successes, open_incident_id,
	last_checked_at, next_check_at, created_at`

func scanMonitor(row pgx.Row) (Monitor, error) {
	var m Monitor
	err := row.Scan(&m.ID, &m.TenantID, &m.Name, &m.Slug, &m.Kind, &m.URL, &m.SimulatedMode, &m.IntervalSeconds, &m.TimeoutMS,
		&m.ExpectedStatusMin, &m.ExpectedStatusMax, &m.ExpectedText, &m.FailureThreshold, &m.RecoveryThreshold, &m.Public,
		&m.Paused, &m.AllowPrivateNetwork, &m.Health, &m.ConsecutiveFailures, &m.ConsecutiveSuccess, &m.OpenIncidentID,
		&m.LastCheckedAt, &m.NextCheckAt, &m.CreatedAt)
	return m, notFound(err)
}

func collectMonitors(rows pgx.Rows, err error) ([]Monitor, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MonitorInput is what an owner (or a sandbox visitor) may set; everything else is state.
type MonitorInput struct {
	Name                string  `json:"name"`
	Slug                string  `json:"slug"`
	Kind                string  `json:"kind"`
	URL                 *string `json:"url"`
	SimulatedMode       string  `json:"simulatedMode"`
	IntervalSeconds     int     `json:"intervalSeconds"`
	TimeoutMS           int     `json:"timeoutMs"`
	ExpectedStatusMin   int     `json:"expectedStatusMin"`
	ExpectedStatusMax   int     `json:"expectedStatusMax"`
	ExpectedText        *string `json:"expectedText"`
	FailureThreshold    int     `json:"failureThreshold"`
	RecoveryThreshold   int     `json:"recoveryThreshold"`
	Public              bool    `json:"public"`
	Paused              bool    `json:"paused"`
	AllowPrivateNetwork bool    `json:"allowPrivateNetwork"`
}

func CreateMonitor(ctx context.Context, tx pgx.Tx, tenantID string, in MonitorInput) (Monitor, error) {
	return scanMonitor(tx.QueryRow(ctx, `INSERT INTO monitors (tenant_id, name, slug, kind, url, simulated_mode, interval_seconds,
		timeout_ms, expected_status_min, expected_status_max, expected_text, failure_threshold, recovery_threshold, is_public, paused,
		allow_private_network)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING `+monitorColumns,
		tenantID, in.Name, in.Slug, in.Kind, in.URL, in.SimulatedMode, in.IntervalSeconds, in.TimeoutMS, in.ExpectedStatusMin,
		in.ExpectedStatusMax, in.ExpectedText, in.FailureThreshold, in.RecoveryThreshold, in.Public, in.Paused, in.AllowPrivateNetwork))
}

func UpdateMonitor(ctx context.Context, tx pgx.Tx, id string, in MonitorInput) (Monitor, error) {
	return scanMonitor(tx.QueryRow(ctx, `UPDATE monitors SET name = $2, slug = $3, kind = $4, url = $5, simulated_mode = $6,
		interval_seconds = $7, timeout_ms = $8, expected_status_min = $9, expected_status_max = $10, expected_text = $11,
		failure_threshold = $12, recovery_threshold = $13, is_public = $14, paused = $15, allow_private_network = $16
		WHERE id = $1 RETURNING `+monitorColumns,
		id, in.Name, in.Slug, in.Kind, in.URL, in.SimulatedMode, in.IntervalSeconds, in.TimeoutMS, in.ExpectedStatusMin,
		in.ExpectedStatusMax, in.ExpectedText, in.FailureThreshold, in.RecoveryThreshold, in.Public, in.Paused, in.AllowPrivateNetwork))
}

func GetMonitor(ctx context.Context, tx pgx.Tx, id string) (Monitor, error) {
	return scanMonitor(tx.QueryRow(ctx, `SELECT `+monitorColumns+` FROM monitors WHERE id = $1`, id))
}

func ListMonitors(ctx context.Context, tx pgx.Tx, publicOnly bool) ([]Monitor, error) {
	return collectMonitors(tx.Query(ctx, `SELECT `+monitorColumns+` FROM monitors WHERE is_public OR NOT $1 ORDER BY name`, publicOnly))
}

func DeleteMonitor(ctx context.Context, tx pgx.Tx, id string) error {
	tag, err := tx.Exec(ctx, `DELETE FROM monitors WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

type Due struct{ MonitorID, TenantID string }

// DueMonitors lists monitors due for a check, across tenants. With slack, a monitor due within
// that long (and within a tenth of its own interval) counts as due already: a schedule driven
// from outside arrives at fixed times, and would otherwise find a monitor a few seconds short of
// due and leave it for a whole extra period.
func DueMonitors(ctx context.Context, pool *pgxpool.Pool, limit int, slack time.Duration) ([]Due, error) {
	rows, err := pool.Query(ctx, `SELECT id, tenant_id FROM lighthouse_due_monitors($1, make_interval(secs => $2))`, limit, slack.Seconds())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Due, error) {
		var d Due
		return d, r.Scan(&d.MonitorID, &d.TenantID)
	})
}

// DueSimulated lists the tenant's simulated monitors that are due now.
func DueSimulated(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM monitors WHERE kind = 'simulated' AND NOT paused AND next_check_at <= now()
		ORDER BY next_check_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// MakeDue brings a monitor's next check forward to now, so it can be claimed at once.
func MakeDue(ctx context.Context, tx pgx.Tx, id string) error {
	tag, err := tx.Exec(ctx, `UPDATE monitors SET next_check_at = now() WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ClearTenant removes the tenant's monitors and incidents (and with them its checks and
// timelines), leaving the tenant and its sessions.
func ClearTenant(ctx context.Context, tx pgx.Tx) error {
	// Monitors point at their open incident, and incidents at their monitor: let go of one side.
	if _, err := tx.Exec(ctx, `UPDATE monitors SET open_incident_id = NULL WHERE open_incident_id IS NOT NULL`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM incidents`); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM monitors`)
	return err
}

// ClaimMonitor moves a due monitor's next check into the future and returns it. Only one caller
// wins: a monitor already claimed (by another worker or instance) returns ErrNotFound. slack is
// as in DueMonitors.
func ClaimMonitor(ctx context.Context, tx pgx.Tx, id string, slack time.Duration) (Monitor, error) {
	return scanMonitor(tx.QueryRow(ctx, `UPDATE monitors SET next_check_at = now() + make_interval(secs => interval_seconds)
		WHERE id = $1 AND NOT paused
		  AND next_check_at <= now() + least(make_interval(secs => $2), make_interval(secs => interval_seconds / 10.0))
		RETURNING `+monitorColumns, id, slack.Seconds()))
}

// LockMonitor reads a monitor for update, so state changes from concurrent checks serialize.
func LockMonitor(ctx context.Context, tx pgx.Tx, id string) (Monitor, error) {
	return scanMonitor(tx.QueryRow(ctx, `SELECT `+monitorColumns+` FROM monitors WHERE id = $1 FOR UPDATE`, id))
}

func SaveMonitorState(ctx context.Context, tx pgx.Tx, m Monitor) error {
	_, err := tx.Exec(ctx, `UPDATE monitors SET health = $2, consecutive_failures = $3, consecutive_successes = $4,
		open_incident_id = $5, last_checked_at = $6 WHERE id = $1`,
		m.ID, m.Health, m.ConsecutiveFailures, m.ConsecutiveSuccess, m.OpenIncidentID, m.LastCheckedAt)
	return err
}

func SetSimulatedMode(ctx context.Context, tx pgx.Tx, id, mode string) (Monitor, error) {
	return scanMonitor(tx.QueryRow(ctx, `UPDATE monitors SET simulated_mode = $2, next_check_at = now()
		WHERE id = $1 AND kind = 'simulated' RETURNING `+monitorColumns, id, mode))
}
