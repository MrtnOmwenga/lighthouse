package monitor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrtnOmwenga/lighthouse/internal/store"
)

// Scheduler finds monitors that are due, checks them with a bounded pool of workers, and records
// the results. Several instances can run against one database: claiming a monitor is atomic, so
// each check runs once.
type Scheduler struct {
	Pool    *pgxpool.Pool
	Prober  *Prober
	Workers int
	Log     *slog.Logger
	Tick    time.Duration // how often to look for due monitors (default 1s)
	// Slack treats a monitor as due this long ahead of time (never more than a tenth of its
	// interval). Zero for Run, which looks every second; set it when rounds are started from
	// outside at fixed times, where a monitor a moment short of due would wait a whole period.
	Slack time.Duration
	// Notify, if set, hears about incidents opened or resolved automatically, after the change
	// is committed. It runs on the checking worker, so it should be quick or time-limited.
	Notify func(ctx context.Context, c Change)
}

// Change is an incident that a check just opened or resolved.
type Change struct {
	TenantID   string
	Opened     bool // false: resolved
	IncidentID string
	Title      string
	Monitor    string
	Public     bool
	StartedAt  time.Time
	At         time.Time
	Failure    Failure // why the last check failed, when opened
}

// Run checks due monitors until ctx is cancelled, then waits for checks in flight.
func (s *Scheduler) Run(ctx context.Context) {
	tick := s.Tick
	if tick == 0 {
		tick = time.Second
	}
	sem := make(chan struct{}, max(s.Workers, 1))
	var wg sync.WaitGroup
	defer wg.Wait()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		s.dispatch(ctx, sem, &wg)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce checks everything due now and returns when those checks are recorded: the whole of a
// schedule driven from outside. Due monitors are listed a batch at a time, so it keeps going until
// a batch holds nothing new; each monitor is checked at most once, however short its interval and
// however long the round takes.
func (s *Scheduler) RunOnce(ctx context.Context) int {
	sem := make(chan struct{}, max(s.Workers, 1))
	var wg sync.WaitGroup
	seen := map[string]bool{}
	for ctx.Err() == nil {
		due, err := store.DueMonitors(ctx, s.Pool, cap(sem)*4, s.Slack)
		if err != nil {
			if ctx.Err() == nil {
				s.Log.Error("listing due monitors", "err", err)
			}
			break
		}
		var fresh []store.Due
		for _, d := range due {
			if !seen[d.MonitorID] {
				seen[d.MonitorID] = true
				fresh = append(fresh, d)
			}
		}
		if len(fresh) == 0 {
			break
		}
		s.start(ctx, fresh, sem, &wg)
		wg.Wait()
	}
	wg.Wait()
	return len(seen)
}

// RunTenant checks one tenant's simulated monitors that are due now, and returns when they are
// recorded. With a schedule driven from outside, nothing else would check a sandbox between
// rounds; the console calls this as it polls, so a visitor watching their sandbox keeps it
// running. Simulated checks send no traffic and take no time, so this is safe on a request.
func (s *Scheduler) RunTenant(ctx context.Context, tenantID string) int {
	var ids []string
	err := store.WithTenant(ctx, s.Pool, tenantID, func(tx pgx.Tx) (err error) {
		ids, err = store.DueSimulated(ctx, tx)
		return err
	})
	if err != nil {
		if ctx.Err() == nil {
			s.Log.Error("listing a tenant's due monitors", "err", err)
		}
		return 0
	}
	due := make([]store.Due, len(ids))
	for i, id := range ids {
		due[i] = store.Due{MonitorID: id, TenantID: tenantID}
	}
	sem := make(chan struct{}, max(s.Workers, 1))
	var wg sync.WaitGroup
	n := s.start(ctx, due, sem, &wg)
	wg.Wait()
	return n
}

// ErrPaused is returned when a check is asked for on a paused monitor.
var ErrPaused = errors.New("monitor is paused")

// CheckNow checks one monitor at once, whatever its schedule, records the result like any other
// check, and returns it. The next scheduled check is a full interval later.
func (s *Scheduler) CheckNow(ctx context.Context, tenantID, monitorID string) (store.Check, error) {
	var none store.Check
	err := store.WithTenant(ctx, s.Pool, tenantID, func(tx pgx.Tx) error {
		m, err := store.GetMonitor(ctx, tx, monitorID)
		if err != nil {
			return err
		}
		if m.Paused {
			return ErrPaused
		}
		return store.MakeDue(ctx, tx, monitorID)
	})
	if err != nil {
		return none, err
	}
	if err := s.Check(ctx, tenantID, monitorID); err != nil {
		return none, err
	}
	var checks []store.Check
	err = store.WithTenant(ctx, s.Pool, tenantID, func(tx pgx.Tx) (err error) {
		checks, err = store.RecentChecks(ctx, tx, monitorID, 0, 1)
		return err
	})
	if err != nil || len(checks) == 0 {
		return none, err
	}
	return checks[0], nil
}

// Try probes with settings that haven't been saved, and records nothing. It goes through the same
// prober as a scheduled check, so the same address guard applies.
func (s *Scheduler) Try(ctx context.Context, in store.MonitorInput) Result {
	return s.probe(ctx, store.Monitor{
		Kind: in.Kind, URL: in.URL, SimulatedMode: in.SimulatedMode, TimeoutMS: in.TimeoutMS,
		ExpectedStatusMin: in.ExpectedStatusMin, ExpectedStatusMax: in.ExpectedStatusMax, ExpectedText: in.ExpectedText,
		AllowPrivateNetwork: in.AllowPrivateNetwork,
	})
}

// dispatch starts a check for each due monitor, as many at a time as there are workers, waiting
// for a free worker when all are busy.
func (s *Scheduler) dispatch(ctx context.Context, sem chan struct{}, wg *sync.WaitGroup) int {
	due, err := store.DueMonitors(ctx, s.Pool, cap(sem)*4, s.Slack)
	if err != nil {
		if ctx.Err() == nil {
			s.Log.Error("listing due monitors", "err", err)
		}
		return 0
	}
	return s.start(ctx, due, sem, wg)
}

func (s *Scheduler) start(ctx context.Context, due []store.Due, sem chan struct{}, wg *sync.WaitGroup) int {
	started := 0
	for _, d := range due {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return started
		}
		started++
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.Check(ctx, d.TenantID, d.MonitorID); err != nil && ctx.Err() == nil {
				s.Log.Error("check failed", "monitor", d.MonitorID, "err", err)
			}
		}()
	}
	return started
}

// Check claims one monitor, probes it outside any transaction (a probe can take up to 30 s), then
// records the result and applies any state change in one transaction.
func (s *Scheduler) Check(ctx context.Context, tenantID, monitorID string) error {
	var m store.Monitor
	err := store.WithTenant(ctx, s.Pool, tenantID, func(tx pgx.Tx) error {
		var err error
		m, err = store.ClaimMonitor(ctx, tx, monitorID, s.Slack)
		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil // another worker got there first, or it was paused or deleted
	}
	if err != nil {
		return fmt.Errorf("claim: %w", err)
	}

	result := s.probe(ctx, m)
	at := time.Now()
	var change *Change
	err = store.WithTenant(ctx, s.Pool, tenantID, func(tx pgx.Tx) (err error) {
		change, err = record(ctx, tx, tenantID, monitorID, result, at)
		return err
	})
	if err == nil && change != nil && s.Notify != nil {
		s.Notify(ctx, *change)
	}
	return err
}

func (s *Scheduler) probe(ctx context.Context, m store.Monitor) Result {
	if m.Kind == "simulated" {
		return s.Prober.Simulated(m.SimulatedMode)
	}
	t := Target{
		Timeout:      time.Duration(m.TimeoutMS) * time.Millisecond,
		StatusMin:    m.ExpectedStatusMin,
		StatusMax:    m.ExpectedStatusMax,
		AllowPrivate: m.AllowPrivateNetwork,
	}
	if m.URL != nil {
		t.URL = *m.URL
	}
	if m.ExpectedText != nil {
		t.ExpectedText = *m.ExpectedText
	}
	return s.Prober.HTTP(ctx, t)
}

// record stores a check and folds it into the monitor's state, opening or resolving its automatic
// incident, and reports that change. The monitor row is locked, so two results for one monitor
// can't interleave.
func record(ctx context.Context, tx pgx.Tx, tenantID, monitorID string, r Result, at time.Time) (*Change, error) {
	m, err := store.LockMonitor(ctx, tx, monitorID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil // deleted while the probe ran
	}
	if err != nil {
		return nil, err
	}
	c := store.Check{MonitorID: m.ID, At: at, OK: r.OK, LatencyMS: int(r.Latency.Milliseconds()), TLSExpiresAt: r.TLSExpiresAt}
	if r.StatusCode != 0 {
		c.StatusCode = &r.StatusCode
	}
	if r.Failure != "" {
		f := string(r.Failure)
		c.Failure = &f
	}
	if _, err := store.InsertCheck(ctx, tx, tenantID, c); err != nil {
		return nil, fmt.Errorf("insert check: %w", err)
	}

	next, transition := Next(
		State{Health: Health(m.Health), Failures: m.ConsecutiveFailures, Successes: m.ConsecutiveSuccess},
		r.OK,
		Thresholds{Failure: m.FailureThreshold, Recovery: m.RecoveryThreshold},
	)
	m.Health, m.ConsecutiveFailures, m.ConsecutiveSuccess, m.LastCheckedAt = string(next.Health), next.Failures, next.Successes, &at

	var change *Change
	switch transition {
	case Opened:
		inc, err := store.CreateIncident(ctx, tx, tenantID, store.Incident{
			MonitorID: &m.ID, Title: m.Name + " is down", Severity: "high", Automatic: true, Public: m.Public, StartedAt: at,
		})
		if err != nil {
			return nil, fmt.Errorf("open incident: %w", err)
		}
		if _, err := store.AddEvent(ctx, tx, tenantID, store.Event{
			IncidentID: inc.ID, At: at, Kind: "opened", Public: true, Author: "Lighthouse",
			Message: fmt.Sprintf("%d checks in a row failed (%s).", next.Failures, r.Failure),
		}); err != nil {
			return nil, err
		}
		m.OpenIncidentID = &inc.ID
		change = &Change{TenantID: tenantID, Opened: true, IncidentID: inc.ID, Title: inc.Title, Monitor: m.Name,
			Public: inc.Public, StartedAt: at, At: at, Failure: r.Failure}
	case Resolved:
		if m.OpenIncidentID != nil {
			inc, resolved, err := resolve(ctx, tx, tenantID, *m.OpenIncidentID, next.Successes, at)
			if err != nil {
				return nil, err
			}
			if resolved {
				change = &Change{TenantID: tenantID, IncidentID: inc.ID, Title: inc.Title, Monitor: m.Name,
					Public: inc.Public, StartedAt: inc.StartedAt, At: at}
			}
		}
		m.OpenIncidentID = nil
	}
	return change, store.SaveMonitorState(ctx, tx, m)
}

// resolve closes an automatic incident when its monitor recovers, unless someone already resolved
// it by hand. It reports whether it did.
func resolve(ctx context.Context, tx pgx.Tx, tenantID, incidentID string, successes int, at time.Time) (store.Incident, bool, error) {
	inc, err := store.GetIncident(ctx, tx, incidentID)
	if errors.Is(err, store.ErrNotFound) {
		return inc, false, nil
	}
	if err != nil {
		return inc, false, err
	}
	if inc.Status == "resolved" {
		return inc, false, nil
	}
	if inc, err = store.SetIncidentStatus(ctx, tx, incidentID, "resolved", at); err != nil {
		return inc, false, fmt.Errorf("resolve incident: %w", err)
	}
	_, err = store.AddEvent(ctx, tx, tenantID, store.Event{
		IncidentID: incidentID, At: at, Kind: "resolved", Public: true, Author: "Lighthouse",
		Message: fmt.Sprintf("Recovered: %d checks in a row passed.", successes),
	})
	return inc, err == nil, err
}

// Prune deletes old checks and visit records, expired sandboxes and expired sessions every hour.
func Prune(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, keepChecks, keepSandboxes time.Duration) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		PruneOnce(ctx, pool, log, keepChecks, keepSandboxes)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// PruneOnce is one round of Prune, for schedules driven from outside (see config.Schedule).
func PruneOnce(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, keepChecks, keepSandboxes time.Duration) {
	if n, err := store.Prune(ctx, pool, keepChecks, keepSandboxes); err != nil {
		if ctx.Err() == nil {
			log.Error("pruning", "err", err)
		}
	} else if n.Checks+n.Sandboxes+n.Sessions > 0 {
		log.Info("pruned", "checks", n.Checks, "sandboxes", n.Sandboxes, "sessions", n.Sessions)
	}
	if views, err := store.PruneAnalytics(ctx, pool, keepChecks); err != nil {
		if ctx.Err() == nil {
			log.Error("pruning visits", "err", err)
		}
	} else if views > 0 {
		log.Info("pruned", "page_views", views)
	}
}
