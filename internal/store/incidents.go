package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Incident struct {
	ID         string     `json:"id"`
	MonitorID  *string    `json:"monitorId"`
	Title      string     `json:"title"`
	Severity   string     `json:"severity"`
	Status     string     `json:"status"`
	Automatic  bool       `json:"automatic"`
	Public     bool       `json:"public"`
	StartedAt  time.Time  `json:"startedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}

type Event struct {
	ID         int64     `json:"id"`
	IncidentID string    `json:"incidentId"`
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"`
	Message    string    `json:"message"`
	Public     bool      `json:"public"`
	Author     string    `json:"author"`
}

const incidentColumns = `id, monitor_id, title, severity, status, automatic, public, started_at, resolved_at`

func scanIncident(row pgx.Row) (Incident, error) {
	var i Incident
	err := row.Scan(&i.ID, &i.MonitorID, &i.Title, &i.Severity, &i.Status, &i.Automatic, &i.Public, &i.StartedAt, &i.ResolvedAt)
	return i, notFound(err)
}

func CreateIncident(ctx context.Context, tx pgx.Tx, tenantID string, i Incident) (Incident, error) {
	return scanIncident(tx.QueryRow(ctx, `INSERT INTO incidents (tenant_id, monitor_id, title, severity, automatic, public, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+incidentColumns, tenantID, i.MonitorID, i.Title, i.Severity, i.Automatic, i.Public, i.StartedAt))
}

func GetIncident(ctx context.Context, tx pgx.Tx, id string) (Incident, error) {
	return scanIncident(tx.QueryRow(ctx, `SELECT `+incidentColumns+` FROM incidents WHERE id = $1`, id))
}

// SetIncidentStatus changes the status; resolved_at follows it (set on resolve, cleared on reopen).
func SetIncidentStatus(ctx context.Context, tx pgx.Tx, id, status string, at time.Time) (Incident, error) {
	return scanIncident(tx.QueryRow(ctx, `UPDATE incidents SET status = $2,
		resolved_at = CASE WHEN $2 = 'resolved' THEN coalesce(resolved_at, $3) ELSE NULL END
		WHERE id = $1 RETURNING `+incidentColumns, id, status, at))
}

func SetIncidentSeverity(ctx context.Context, tx pgx.Tx, id, severity string) (Incident, error) {
	return scanIncident(tx.QueryRow(ctx, `UPDATE incidents SET severity = $2 WHERE id = $1 RETURNING `+incidentColumns, id, severity))
}

// IncidentCursor is the (startedAt, id) of the last incident on the previous page.
type IncidentCursor struct {
	StartedAt time.Time
	ID        string
}

// IncidentFilter selects incidents that started since Since, plus any still unresolved.
type IncidentFilter struct {
	Since      time.Time
	Before     *IncidentCursor
	PublicOnly bool
	Limit      int
}

// ListIncidents lists incidents newest first, a page at a time (keyset pagination).
func ListIncidents(ctx context.Context, tx pgx.Tx, f IncidentFilter) ([]Incident, error) {
	var beforeAt *time.Time
	var beforeID *string
	if f.Before != nil {
		beforeAt, beforeID = &f.Before.StartedAt, &f.Before.ID
	}
	rows, err := tx.Query(ctx, `SELECT `+incidentColumns+` FROM incidents
		WHERE (started_at >= $1 OR status <> 'resolved')
		  AND ($2::timestamptz IS NULL OR (started_at, id) < ($2, $3::uuid))
		  AND (public OR NOT $4)
		ORDER BY started_at DESC, id DESC LIMIT $5`, f.Since, beforeAt, beforeID, f.PublicOnly, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Incident, error) { return scanIncident(r) })
}

func AddEvent(ctx context.Context, tx pgx.Tx, tenantID string, e Event) (Event, error) {
	err := tx.QueryRow(ctx, `INSERT INTO incident_events (tenant_id, incident_id, at, kind, message, public, author)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, tenantID, e.IncidentID, e.At, e.Kind, e.Message, e.Public, e.Author).Scan(&e.ID)
	return e, err
}

// OpenIncidents counts the tenant's incidents that aren't resolved.
func OpenIncidents(ctx context.Context, tx pgx.Tx) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM incidents WHERE status <> 'resolved'`).Scan(&n)
	return n, err
}

func Events(ctx context.Context, tx pgx.Tx, incidentID string, publicOnly bool) ([]Event, error) {
	rows, err := tx.Query(ctx, `SELECT id, incident_id, at, kind, message, public, author FROM incident_events
		WHERE incident_id = $1 AND (public OR NOT $2) ORDER BY at, id`, incidentID, publicOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		var e Event
		return e, r.Scan(&e.ID, &e.IncidentID, &e.At, &e.Kind, &e.Message, &e.Public, &e.Author)
	})
}
