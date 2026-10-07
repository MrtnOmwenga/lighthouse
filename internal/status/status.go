// Package status builds the public status page: what is up, how reliable it has been, and what
// went wrong. Everything on it is derived from public monitors, public incidents and their public
// timeline events only.
package status

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrtnOmwenga/lighthouse/internal/store"
)

type Overall string

const (
	Operational   Overall = "operational"
	Degraded      Overall = "degraded" // an open incident, or some monitors down
	MajorOutage   Overall = "major_outage"
	NoMonitorsYet Overall = "no_monitors"
)

const Days = 90

type Page struct {
	Overall   Overall    `json:"overall"`
	Monitors  []Monitor  `json:"monitors"`
	Active    []Incident `json:"activeIncidents"`
	Recent    []Incident `json:"recentIncidents"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

type Monitor struct {
	ID          string        `json:"-"`
	Name        string        `json:"name"`
	Slug        string        `json:"slug"`
	Health      string        `json:"health"`
	Checks24h   int           `json:"checks24h"`
	Uptime24h   *float64      `json:"uptime24h"` // percent; nil without data
	Uptime7d    *float64      `json:"uptime7d"`
	Uptime90d   *float64      `json:"uptime90d"`
	P50         *float64      `json:"p50Ms"`
	P95         *float64      `json:"p95Ms"`
	Days        []Day         `json:"days"` // oldest first, exactly Days of them
	Latency     []store.Point `json:"-"`
	LastChecked *time.Time    `json:"lastCheckedAt"`
}

type Day struct {
	Date   string   `json:"date"`
	Uptime *float64 `json:"uptime"` // nil: no checks that day
}

type Incident struct {
	store.Incident
	Events []store.Event `json:"events"`
}

// Build assembles the status page for a tenant.
func Build(ctx context.Context, pool *pgxpool.Pool, tenantID string, now time.Time) (Page, error) {
	return build(ctx, pool, tenantID, now, true)
}

// BuildAll is Build over everything the tenant has, private monitors and incidents included: the
// tenant's own view of its figures, for its console.
func BuildAll(ctx context.Context, pool *pgxpool.Pool, tenantID string, now time.Time) (Page, error) {
	return build(ctx, pool, tenantID, now, false)
}

func build(ctx context.Context, pool *pgxpool.Pool, tenantID string, now time.Time, publicOnly bool) (Page, error) {
	page := Page{UpdatedAt: now, Monitors: []Monitor{}, Active: []Incident{}, Recent: []Incident{}}
	err := store.WithTenant(ctx, pool, tenantID, func(tx pgx.Tx) error {
		monitors, err := store.ListMonitors(ctx, tx, publicOnly)
		if err != nil {
			return err
		}
		ids := make([]string, len(monitors))
		for i, m := range monitors {
			ids[i] = m.ID
		}
		uptime, err := store.UptimeStats(ctx, tx, ids, now)
		if err != nil {
			return err
		}
		daily, err := store.DailyStats(ctx, tx, ids, Days, now)
		if err != nil {
			return err
		}
		series, err := store.LatencySeries(ctx, tx, ids, 24*time.Hour, 30*time.Minute, now)
		if err != nil {
			return err
		}
		for _, m := range monitors {
			u := uptime[m.ID]
			page.Monitors = append(page.Monitors, Monitor{
				ID: m.ID, Name: m.Name, Slug: m.Slug, Health: m.Health, LastChecked: m.LastCheckedAt, Checks24h: u.Checks24h,
				Uptime24h: Percent(u.OK24h, u.Checks24h), Uptime7d: Percent(u.OK7d, u.Checks7d), Uptime90d: Percent(u.OK90d, u.Checks90d),
				P50: round(u.P50), P95: round(u.P95),
				Days: fillDays(daily[m.ID], now), Latency: series[m.ID],
			})
		}

		incidents, err := store.ListIncidents(ctx, tx, store.IncidentFilter{Since: now.AddDate(0, 0, -14), PublicOnly: publicOnly, Limit: 50})
		if err != nil {
			return err
		}
		for _, inc := range incidents {
			events, err := store.Events(ctx, tx, inc.ID, publicOnly)
			if err != nil {
				return err
			}
			view := Incident{Incident: inc, Events: events}
			if inc.Status == "resolved" {
				page.Recent = append(page.Recent, view)
			} else {
				page.Active = append(page.Active, view)
			}
		}
		return nil
	})
	page.Overall = overall(page.Monitors, len(page.Active))
	return page, err
}

func overall(monitors []Monitor, active int) Overall {
	if len(monitors) == 0 && active == 0 {
		return NoMonitorsYet
	}
	down := 0
	for _, m := range monitors {
		if m.Health == "down" {
			down++
		}
	}
	switch {
	case len(monitors) > 0 && down == len(monitors):
		return MajorOutage
	case down > 0 || active > 0:
		return Degraded
	default:
		return Operational
	}
}

// Percent is ok/total as a percentage, rounded down to two decimals so that anything short of
// perfect never displays as 100%.
func Percent(ok, total int) *float64 {
	if total == 0 {
		return nil
	}
	p := math.Floor(float64(ok)/float64(total)*10000) / 100
	return &p
}

func round(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := math.Round(*v)
	return &r
}

// fillDays turns the days that have checks into exactly Days entries, one per UTC day.
func fillDays(days []store.Day, now time.Time) []Day {
	byDate := map[string]store.Day{}
	for _, d := range days {
		byDate[d.Date.Format(time.DateOnly)] = d
	}
	today := now.UTC().Truncate(24 * time.Hour)
	out := make([]Day, Days)
	for i := range Days {
		date := today.AddDate(0, 0, i-Days+1).Format(time.DateOnly)
		d := byDate[date]
		out[i] = Day{Date: date, Uptime: Percent(d.OK, d.Checks)}
	}
	return out
}

// Label describes the overall state in words.
func (o Overall) Label() string {
	switch o {
	case Operational:
		return "All systems operational"
	case Degraded:
		return "Some systems are having problems"
	case MajorOutage:
		return "Major outage"
	default:
		return "Nothing monitored yet"
	}
}

// Bar is the CSS class for one day of the 90-day history.
func (d Day) Bar() string {
	switch {
	case d.Uptime == nil:
		return "none"
	case *d.Uptime >= 99.5:
		return "good"
	case *d.Uptime >= 95:
		return "fair"
	default:
		return "bad"
	}
}

func (d Day) Title() string {
	if d.Uptime == nil {
		return d.Date + ": no data"
	}
	return fmt.Sprintf("%s: %.2f%% uptime", d.Date, *d.Uptime)
}
