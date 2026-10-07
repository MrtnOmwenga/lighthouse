package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Uptime is one monitor's check counts over the standard windows, and its latency over 24 hours.
type Uptime struct {
	Checks24h, OK24h int
	Checks7d, OK7d   int
	Checks90d, OK90d int
	P50, P95         *float64 // milliseconds, passing checks in the last 24 hours, warm-ups left out
}

func UptimeStats(ctx context.Context, tx pgx.Tx, monitorIDs []string, now time.Time) (map[string]Uptime, error) {
	rows, err := tx.Query(ctx, `SELECT monitor_id,
			count(*) FILTER (WHERE at > $2::timestamptz - interval '24 hours'),
			count(*) FILTER (WHERE ok AND at > $2::timestamptz - interval '24 hours'),
			count(*) FILTER (WHERE at > $2::timestamptz - interval '7 days'),
			count(*) FILTER (WHERE ok AND at > $2::timestamptz - interval '7 days'),
			count(*),
			count(*) FILTER (WHERE ok),
			percentile_cont(0.5) WITHIN GROUP (ORDER BY latency_ms) FILTER (WHERE ok AND NOT warmup AND at > $2::timestamptz - interval '24 hours'),
			percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms) FILTER (WHERE ok AND NOT warmup AND at > $2::timestamptz - interval '24 hours')
		FROM checks WHERE monitor_id = ANY($1::uuid[]) AND at > $2::timestamptz - interval '90 days' AND at <= $2
		GROUP BY monitor_id`, monitorIDs, now)
	if err != nil {
		return nil, err
	}
	out := map[string]Uptime{}
	var id string
	var u Uptime
	_, err = pgx.ForEachRow(rows, []any{&id, &u.Checks24h, &u.OK24h, &u.Checks7d, &u.OK7d, &u.Checks90d, &u.OK90d, &u.P50, &u.P95},
		func() error { out[id] = u; return nil })
	return out, err
}

// Day is one monitor's checks on one UTC day.
type Day struct {
	Date       time.Time
	Checks, OK int
}

// DailyStats returns each monitor's per-day counts for the last `days` days (UTC), oldest first.
// Days without checks are absent.
func DailyStats(ctx context.Context, tx pgx.Tx, monitorIDs []string, days int, now time.Time) (map[string][]Day, error) {
	rows, err := tx.Query(ctx, `SELECT monitor_id, (at AT TIME ZONE 'UTC')::date AS day, count(*), count(*) FILTER (WHERE ok)
		FROM checks WHERE monitor_id = ANY($1::uuid[])
		  AND at >= (($3::timestamptz AT TIME ZONE 'UTC')::date - ($2::int - 1)) AT TIME ZONE 'UTC' AND at <= $3
		GROUP BY 1, 2 ORDER BY 1, 2`, monitorIDs, days, now)
	if err != nil {
		return nil, err
	}
	out := map[string][]Day{}
	var id string
	var d Day
	_, err = pgx.ForEachRow(rows, []any{&id, &d.Date, &d.Checks, &d.OK}, func() error {
		out[id] = append(out[id], d)
		return nil
	})
	return out, err
}

// Point is one bucket of a latency series.
type Point struct {
	At        time.Time
	LatencyMS *float64 // mean latency of passing checks; nil when none passed
	Failed    int
}

// LatencySeries buckets each monitor's checks over the last `window`, oldest first. Empty buckets
// are absent.
func LatencySeries(ctx context.Context, tx pgx.Tx, monitorIDs []string, window, bucket time.Duration, now time.Time) (map[string][]Point, error) {
	rows, err := tx.Query(ctx, `SELECT monitor_id, date_bin($3::interval, at, $4::timestamptz - $2::interval) AS b,
			avg(latency_ms) FILTER (WHERE ok AND NOT warmup)::float8, count(*) FILTER (WHERE NOT ok)
		FROM checks WHERE monitor_id = ANY($1::uuid[]) AND at > $4::timestamptz - $2::interval AND at <= $4
		GROUP BY 1, 2 ORDER BY 1, 2`, monitorIDs, window, bucket, now)
	if err != nil {
		return nil, err
	}
	out := map[string][]Point{}
	var id string
	var p Point
	_, err = pgx.ForEachRow(rows, []any{&id, &p.At, &p.LatencyMS, &p.Failed}, func() error {
		out[id] = append(out[id], p)
		return nil
	})
	return out, err
}
