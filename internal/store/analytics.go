package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DailySalt returns the salt for day's visitor hashes, creating it on first use and deleting older
// ones.
func DailySalt(ctx context.Context, pool *pgxpool.Pool, day time.Time) ([]byte, error) {
	var salt []byte
	err := pool.QueryRow(ctx, `SELECT lighthouse_daily_salt($1::date)`, day.UTC().Format(time.DateOnly)).Scan(&salt)
	return salt, err
}

type PageView struct {
	ID       string
	At       time.Time
	Visitor  string
	Path     string
	Project  *string
	Ref      *string
	Referrer *string
	Device   string
}

// InsertView records a page view. Replaying the same view ID is a no-op.
func InsertView(ctx context.Context, tx pgx.Tx, tenantID string, v PageView) error {
	_, err := tx.Exec(ctx, `INSERT INTO page_views (id, tenant_id, at, day, visitor, path, project, ref, referrer, device)
		VALUES ($1, $2, $3, ($3 AT TIME ZONE 'UTC')::date, $4, $5, $6, $7, $8, $9) ON CONFLICT (id) DO NOTHING`,
		v.ID, tenantID, v.At, v.Visitor, v.Path, v.Project, v.Ref, v.Referrer, v.Device)
	return err
}

// TagViewsOn counts the views recorded on at's UTC day that carry the tag.
func TagViewsOn(ctx context.Context, tx pgx.Tx, tag string, at time.Time) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM page_views WHERE ref = $1 AND day = ($2::timestamptz AT TIME ZONE 'UTC')::date`, tag, at).Scan(&n)
	return n, err
}

// MaxPingCredit is the most engaged time one heartbeat can add. Browsers send one every 15
// seconds while someone is actually using the page; the server measures the gap itself, so a
// client can't claim more time than has passed, and time spent away is never counted.
const MaxPingCredit = 20

// Ping credits a view with the engaged time since its last heartbeat, capped. Views older than two
// hours accept no more time.
func Ping(ctx context.Context, tx pgx.Tx, viewID string) error {
	_, err := tx.Exec(ctx, `UPDATE page_views SET
			engaged_seconds = least(3600, engaged_seconds + least($2, greatest(0, floor(extract(epoch FROM now() - last_ping))::int))),
			last_ping = now()
		WHERE id = $1 AND at > now() - interval '2 hours'`, viewID, MaxPingCredit)
	return err
}

// InsertEvent records something a visitor did on a view (at most once per kind). An unknown view
// is ignored.
func InsertEvent(ctx context.Context, tx pgx.Tx, tenantID, viewID, name string) error {
	_, err := tx.Exec(ctx, `INSERT INTO analytics_events (tenant_id, view_id, name) VALUES ($1, $2, $3) ON CONFLICT (view_id, name) DO NOTHING`,
		tenantID, viewID, name)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23503" {
		return nil
	}
	return err
}

func PruneAnalytics(ctx context.Context, pool *pgxpool.Pool, keep time.Duration) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT lighthouse_prune_analytics($1)`, keep).Scan(&n)
	return n, err
}

// EngagedSeconds is how long a page must have been actively read for its visitor to count as
// engaged. A view is recorded the moment a page loads, so a glance, a mis-click and a scanner
// driving a real browser all look alike: one view, no reading. Opening a demo also counts.
const EngagedSeconds = 5

// engagedView is that test for a page_views row named v. A demo becoming ready happens by itself
// on a launch page, so it isn't a sign of anyone being there.
var engagedView = fmt.Sprintf(`(v.engaged_seconds >= %d OR EXISTS
	(SELECT 1 FROM analytics_events x WHERE x.view_id = v.id AND x.name <> 'demo_ready'))`, EngagedSeconds)

// Summary is the whole site's readership: everyone, and those who actually read something.
type Summary struct {
	Views           int `json:"views"`
	Visitors        int `json:"visitors"`
	EngagedVisitors int `json:"engagedVisitors"`
}

func SiteSummary(ctx context.Context, tx pgx.Tx, since time.Time) (Summary, error) {
	var s Summary
	err := tx.QueryRow(ctx, `SELECT count(*), count(DISTINCT (v.day, v.visitor)),
			count(DISTINCT (v.day, v.visitor)) FILTER (WHERE `+engagedView+`)
		FROM page_views v WHERE v.at >= $1`, since).Scan(&s.Views, &s.Visitors, &s.EngagedVisitors)
	return s, err
}

// PageStat is how one page was read.
type PageStat struct {
	Path            string  `json:"path"`
	Project         *string `json:"project"`
	Views           int     `json:"views"`
	Visitors        int     `json:"visitors"`
	EngagedVisitors int     `json:"engagedVisitors"`
	MedianEngaged   float64 `json:"medianEngagedSeconds"`
}

func PageStats(ctx context.Context, tx pgx.Tx, since time.Time) ([]PageStat, error) {
	rows, err := tx.Query(ctx, `SELECT v.path, min(v.project), count(*), count(DISTINCT (v.day, v.visitor)),
			count(DISTINCT (v.day, v.visitor)) FILTER (WHERE `+engagedView+`),
			coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY v.engaged_seconds), 0)
		FROM page_views v WHERE v.at >= $1 GROUP BY v.path ORDER BY count(*) DESC, v.path`, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PageStat, error) {
		var s PageStat
		return s, r.Scan(&s.Path, &s.Project, &s.Views, &s.Visitors, &s.EngagedVisitors, &s.MedianEngaged)
	})
}

// ProjectStat is how often a project was read about, launched and opened.
type ProjectStat struct {
	Project         string  `json:"project"`
	Views           int     `json:"views"`
	Visitors        int     `json:"visitors"`
	EngagedVisitors int     `json:"engagedVisitors"`
	MedianEngaged   float64 `json:"medianEngagedSeconds"`
	Launches        int     `json:"launches"` // visits to its launch page
	Opens           int     `json:"opens"`    // the demo was actually opened
}

func ProjectStats(ctx context.Context, tx pgx.Tx, since time.Time) ([]ProjectStat, error) {
	rows, err := tx.Query(ctx, `SELECT v.project, count(*), count(DISTINCT (v.day, v.visitor)),
			count(DISTINCT (v.day, v.visitor)) FILTER (WHERE `+engagedView+`),
			coalesce(percentile_cont(0.5) WITHIN GROUP (ORDER BY v.engaged_seconds), 0),
			count(*) FILTER (WHERE v.path LIKE '/go/%'),
			count(e.id)
		FROM page_views v LEFT JOIN analytics_events e ON e.view_id = v.id AND e.name = 'demo_open'
		WHERE v.at >= $1 AND v.project IS NOT NULL
		GROUP BY v.project ORDER BY count(*) DESC, v.project`, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (ProjectStat, error) {
		var s ProjectStat
		return s, r.Scan(&s.Project, &s.Views, &s.Visitors, &s.EngagedVisitors, &s.MedianEngaged, &s.Launches, &s.Opens)
	})
}

// RefStat is what the visitors who arrived through one ?ref= tag did that day: every page they
// viewed, not only the first.
type RefStat struct {
	Ref            string    `json:"ref"`
	Visitors       int       `json:"visitors"`
	FirstSeen      time.Time `json:"firstSeen"`
	LastSeen       time.Time `json:"lastSeen"`
	Views          int       `json:"views"`
	EngagedSeconds int       `json:"engagedSeconds"`
	Pages          []string  `json:"pages"`
	DemosOpened    int       `json:"demosOpened"`
	Actions        []string  `json:"actions"` // what they did besides reading: cv_download, outbound_github, …
}

func RefStats(ctx context.Context, tx pgx.Tx, since time.Time) ([]RefStat, error) {
	// Views first, events apart: joining them in one pass would count a view once per event.
	rows, err := tx.Query(ctx, `WITH tagged AS (
			SELECT DISTINCT ref, day, visitor FROM page_views WHERE ref IS NOT NULL AND at >= $1
		), v AS (
			SELECT t.ref, pv.id, pv.day, pv.visitor, pv.at, pv.path, pv.engaged_seconds
			FROM tagged t JOIN page_views pv ON pv.day = t.day AND pv.visitor = t.visitor
		), ev AS (
			SELECT DISTINCT v.ref, v.id, e.name FROM v JOIN analytics_events e ON e.view_id = v.id
		)
		SELECT v.ref, count(DISTINCT (v.day, v.visitor)), min(v.at), max(v.at), count(DISTINCT v.id),
			(SELECT coalesce(sum(d.engaged_seconds), 0)::int FROM (SELECT DISTINCT id, engaged_seconds FROM v x WHERE x.ref = v.ref) d),
			array_agg(DISTINCT v.path ORDER BY v.path),
			(SELECT count(*) FROM ev WHERE ev.ref = v.ref AND ev.name = 'demo_open'),
			coalesce((SELECT array_agg(DISTINCT ev.name ORDER BY ev.name) FROM ev
				WHERE ev.ref = v.ref AND ev.name NOT IN ('demo_ready', 'demo_open', 'intro_skip')), '{}')
		FROM v GROUP BY v.ref ORDER BY max(v.at) DESC`, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RefStat, error) {
		var s RefStat
		return s, r.Scan(&s.Ref, &s.Visitors, &s.FirstSeen, &s.LastSeen, &s.Views, &s.EngagedSeconds, &s.Pages, &s.DemosOpened, &s.Actions)
	})
}

// Count is a label and how many visitors it had.
type Count struct {
	Label    string `json:"label"`
	Visitors int    `json:"visitors"`
}

// ActionCounts is how many visitors did each thing (opened a demo, took the CV, went on to
// GitHub…). A demo becoming ready happens by itself, so it isn't listed.
func ActionCounts(ctx context.Context, tx pgx.Tx, since time.Time) ([]Count, error) {
	rows, err := tx.Query(ctx, `SELECT e.name, count(DISTINCT (v.day, v.visitor))
		FROM analytics_events e JOIN page_views v ON v.id = e.view_id
		WHERE v.at >= $1 AND e.name <> 'demo_ready' GROUP BY 1 ORDER BY 2 DESC, 1`, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Count, error) {
		var c Count
		return c, r.Scan(&c.Label, &c.Visitors)
	})
}

// Breakdown counts distinct visitors by referrer or device.
func Breakdown(ctx context.Context, tx pgx.Tx, by string, since time.Time) ([]Count, error) {
	column := map[string]string{"referrer": "coalesce(referrer, '(direct)')", "device": "device"}[by]
	if column == "" {
		return nil, errors.New("unknown breakdown")
	}
	rows, err := tx.Query(ctx, `SELECT `+column+`, count(DISTINCT (day, visitor)) FROM page_views WHERE at >= $1
		GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT 20`, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Count, error) {
		var c Count
		return c, r.Scan(&c.Label, &c.Visitors)
	})
}
