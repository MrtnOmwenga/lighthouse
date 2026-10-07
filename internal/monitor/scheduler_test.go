package monitor_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MrtnOmwenga/lighthouse/internal/monitor"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
	"github.com/MrtnOmwenga/lighthouse/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

type fixture struct {
	t      *testing.T
	db     testdb.DB
	tenant string
	sched  *monitor.Scheduler
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testdb.New(t)
	tenant := uuid.NewString()
	if err := store.CreateSandbox(context.Background(), db.App, tenant, func(pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, db: db, tenant: tenant, sched: &monitor.Scheduler{
		Pool: db.App, Prober: monitor.NewProber(), Workers: 4, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}
}

func (f *fixture) do(fn func(tx pgx.Tx) error) {
	f.t.Helper()
	if err := store.WithTenant(context.Background(), f.db.App, f.tenant, fn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) create(in store.MonitorInput) store.Monitor {
	f.t.Helper()
	var m store.Monitor
	f.do(func(tx pgx.Tx) (err error) {
		m, err = store.CreateMonitor(context.Background(), tx, f.tenant, in)
		return
	})
	return m
}

// tick makes every monitor due and runs one round of checks.
func (f *fixture) tick() {
	f.t.Helper()
	if _, err := f.db.Owner.Exec(context.Background(), "UPDATE monitors SET next_check_at = now()"); err != nil {
		f.t.Fatal(err)
	}
	f.sched.RunOnce(context.Background())
}

func (f *fixture) monitor(id string) store.Monitor {
	f.t.Helper()
	var m store.Monitor
	f.do(func(tx pgx.Tx) (err error) { m, err = store.GetMonitor(context.Background(), tx, id); return })
	return m
}

func (f *fixture) incidents() []store.Incident {
	f.t.Helper()
	var list []store.Incident
	f.do(func(tx pgx.Tx) (err error) {
		list, err = store.ListIncidents(context.Background(), tx, store.IncidentFilter{Limit: 100})
		return
	})
	return list
}

func (f *fixture) events(incidentID string) []string {
	f.t.Helper()
	var kinds []string
	f.do(func(tx pgx.Tx) error {
		events, err := store.Events(context.Background(), tx, incidentID, true)
		for _, e := range events {
			kinds = append(kinds, e.Kind)
		}
		return err
	})
	return kinds
}

func input(kind string) store.MonitorInput {
	return store.MonitorInput{
		Name: "Demo", Slug: "demo-" + kind, Kind: kind, SimulatedMode: "up", IntervalSeconds: 60, TimeoutMS: 2000,
		ExpectedStatusMin: 200, ExpectedStatusMax: 299, FailureThreshold: 2, RecoveryThreshold: 2, Public: true,
	}
}

func TestIncidentLifecycle(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	m := f.create(input("simulated"))

	f.tick()
	if got := f.monitor(m.ID); got.Health != "up" || got.LastCheckedAt == nil {
		t.Fatalf("after a passing check: %+v", got)
	}

	f.do(func(tx pgx.Tx) error { _, err := store.SetSimulatedMode(ctx, tx, m.ID, "down"); return err })
	f.tick()
	if got := f.monitor(m.ID); got.Health != "up" || len(f.incidents()) != 0 {
		t.Fatalf("one failure is below the threshold: %+v", got)
	}
	f.tick()
	got := f.monitor(m.ID)
	incs := f.incidents()
	if got.Health != "down" || len(incs) != 1 || got.OpenIncidentID == nil || *got.OpenIncidentID != incs[0].ID {
		t.Fatalf("two failures open an incident: %+v %+v", got, incs)
	}
	if inc := incs[0]; !inc.Automatic || inc.Status != "open" || inc.Title != "Demo is down" || inc.Severity != "high" {
		t.Fatalf("incident: %+v", inc)
	}
	f.tick()
	if len(f.incidents()) != 1 {
		t.Fatal("still down: no second incident")
	}

	f.do(func(tx pgx.Tx) error { _, err := store.SetSimulatedMode(ctx, tx, m.ID, "up"); return err })
	f.tick()
	if f.incidents()[0].Status != "open" {
		t.Fatal("one success is below the recovery threshold")
	}
	f.tick()
	incs = f.incidents()
	if incs[0].Status != "resolved" || incs[0].ResolvedAt == nil || f.monitor(m.ID).OpenIncidentID != nil {
		t.Fatalf("two successes resolve it: %+v", incs[0])
	}
	if kinds := f.events(incs[0].ID); len(kinds) != 2 || kinds[0] != "opened" || kinds[1] != "resolved" {
		t.Fatalf("timeline: %v", kinds)
	}

	var checks []store.Check
	f.do(func(tx pgx.Tx) (err error) { checks, err = store.RecentChecks(ctx, tx, m.ID, 0, 100); return })
	if len(checks) != 6 {
		t.Fatalf("every check is recorded: %d", len(checks))
	}
}

// Someone resolves an automatic incident by hand before the monitor recovers: recovery doesn't
// resolve it a second time.
func TestManualResolveIsRespected(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	in := input("simulated")
	in.SimulatedMode = "down"
	m := f.create(in)
	f.tick()
	f.tick()
	inc := f.incidents()[0]
	f.do(func(tx pgx.Tx) error {
		_, err := store.SetIncidentStatus(ctx, tx, inc.ID, "resolved", time.Now())
		return err
	})
	f.do(func(tx pgx.Tx) error { _, err := store.SetSimulatedMode(ctx, tx, m.ID, "up"); return err })
	f.tick()
	f.tick()
	if kinds := f.events(inc.ID); len(kinds) != 1 {
		t.Fatalf("no automatic 'resolved' after a manual one: %v", kinds)
	}
	if f.monitor(m.ID).Health != "up" {
		t.Fatal("the monitor still recovers")
	}
}

func TestHTTPMonitor(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var status atomic.Int32
	status.Store(200)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(status.Load()))
		_, _ = w.Write([]byte("hello"))
	}))
	defer site.Close()

	in := input("http")
	in.URL = &site.URL
	in.AllowPrivateNetwork = true // the test site is on loopback
	m := f.create(in)
	f.tick()
	if got := f.monitor(m.ID); got.Health != "up" {
		t.Fatalf("health %s", got.Health)
	}
	status.Store(500)
	f.tick()
	f.tick()
	if got := f.monitor(m.ID); got.Health != "down" || len(f.incidents()) != 1 {
		t.Fatalf("a 500 twice takes it down: %+v", got)
	}
	var checks []store.Check
	f.do(func(tx pgx.Tx) (err error) {
		checks, err = store.RecentChecks(context.Background(), tx, m.ID, 0, 1)
		return
	})
	if c := checks[0]; c.OK || c.StatusCode == nil || *c.StatusCode != 500 || c.Failure == nil || *c.Failure != "status" {
		t.Fatalf("latest check: %+v", c)
	}
}

// A monitor pointed at an internal address without permission never reaches it.
func TestPrivateTargetsAreBlocked(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	var hits atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer site.Close()
	in := input("http")
	in.URL = &site.URL
	m := f.create(in)
	f.tick()
	var checks []store.Check
	f.do(func(tx pgx.Tx) (err error) {
		checks, err = store.RecentChecks(context.Background(), tx, m.ID, 0, 1)
		return
	})
	if hits.Load() != 0 || len(checks) != 1 || checks[0].Failure == nil || *checks[0].Failure != "blocked" {
		t.Fatalf("hits %d, checks %+v", hits.Load(), checks)
	}
}

// Several schedulers (instances) racing over the same due monitors check each one exactly once.
func TestConcurrentSchedulersCheckOnce(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i := range 20 {
		in := input("simulated")
		in.Slug = uuid.NewString()[:8] + string(rune('a'+i))
		f.create(in)
	}
	var wg sync.WaitGroup
	for range 4 {
		s := *f.sched
		s.Workers = 8
		wg.Go(func() { s.RunOnce(context.Background()) })
	}
	wg.Wait()
	var monitors, checked, most int
	if err := f.db.Owner.QueryRow(context.Background(), `SELECT count(*), count(c.n), coalesce(max(c.n), 0)
		FROM monitors m LEFT JOIN (SELECT monitor_id, count(*) n FROM checks GROUP BY 1) c ON c.monitor_id = m.id`).
		Scan(&monitors, &checked, &most); err != nil {
		t.Fatal(err)
	}
	if monitors != 20 || checked != 20 || most != 1 {
		t.Fatalf("%d monitors, %d checked, at most %d checks each: want every one checked exactly once", monitors, checked, most)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.create(input("simulated"))
	f.sched.Tick = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.sched.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		_ = f.db.Owner.QueryRow(context.Background(), "SELECT count(*) FROM checks").Scan(&n)
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run never checked the due monitor")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run didn't return after cancel")
	}
}

// The scheduler reports each automatic incident exactly twice (opened, resolved), after the change
// is saved, and never for steady states.
func TestNotifiesOnOpenAndResolveOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()
	var mu sync.Mutex
	var changes []monitor.Change
	f.sched.Notify = func(_ context.Context, c monitor.Change) {
		mu.Lock()
		defer mu.Unlock()
		changes = append(changes, c)
	}
	in := input("simulated")
	in.SimulatedMode = "down"
	m := f.create(in)
	for range 4 {
		f.tick()
	}
	f.do(func(tx pgx.Tx) error { _, err := store.SetSimulatedMode(ctx, tx, m.ID, "up"); return err })
	for range 4 {
		f.tick()
	}
	if len(changes) != 2 || !changes[0].Opened || changes[1].Opened || changes[0].IncidentID != changes[1].IncidentID {
		t.Fatalf("changes: %+v", changes)
	}
	if changes[0].TenantID != f.tenant || changes[0].Monitor != "Demo" || changes[0].Failure != monitor.FailStatus {
		t.Fatalf("opened: %+v", changes[0])
	}
	if !changes[1].At.After(changes[1].StartedAt) {
		t.Fatalf("resolved: %+v", changes[1])
	}
}

// One round checks everything that is due, however many batches that takes, and each monitor once.
func TestRunOnceChecksEverythingDue(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.sched.Workers = 1 // a batch of four
	for i := range 11 {
		in := input("simulated")
		in.Slug = uuid.NewString()[:8] + string(rune('a'+i))
		in.IntervalSeconds = 5
		f.create(in)
	}
	if n := f.sched.RunOnce(context.Background()); n != 11 {
		t.Fatalf("RunOnce checked %d monitors, want 11", n)
	}
	var checked, most int
	if err := f.db.Owner.QueryRow(context.Background(),
		`SELECT count(*), coalesce(max(n), 0) FROM (SELECT count(*) n FROM checks GROUP BY monitor_id) c`).Scan(&checked, &most); err != nil {
		t.Fatal(err)
	}
	if checked != 11 || most != 1 {
		t.Fatalf("%d monitors checked, at most %d times each: want 11, once each", checked, most)
	}
}

// A console read drives its own tenant's simulated checks, and nothing else.
func TestRunTenant(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mine := f.create(input("simulated"))
	real := input("http")
	u := "https://example.com/"
	real.URL = &u
	live := f.create(real)

	other := uuid.NewString()
	if err := store.CreateSandbox(context.Background(), f.db.App, other, func(pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var theirs store.Monitor
	if err := store.WithTenant(context.Background(), f.db.App, other, func(tx pgx.Tx) (err error) {
		theirs, err = store.CreateMonitor(context.Background(), tx, other, input("simulated"))
		return
	}); err != nil {
		t.Fatal(err)
	}

	if n := f.sched.RunTenant(context.Background(), f.tenant); n != 1 {
		t.Fatalf("RunTenant started %d checks, want 1", n)
	}
	if f.monitor(mine.ID).LastCheckedAt == nil {
		t.Fatal("the tenant's simulated monitor wasn't checked")
	}
	if f.monitor(live.ID).LastCheckedAt != nil {
		t.Fatal("an HTTP monitor was probed from a console read")
	}
	var checks int
	if err := f.db.Owner.QueryRow(context.Background(), "SELECT count(*) FROM checks WHERE monitor_id = $1", theirs.ID).Scan(&checks); err != nil || checks != 0 {
		t.Fatalf("another tenant's monitor was checked: %d %v", checks, err)
	}
	// Not due again until its interval has passed.
	if n := f.sched.RunTenant(context.Background(), f.tenant); n != 0 {
		t.Fatalf("a second read straight away started %d checks", n)
	}
}

func (f *fixture) checks(monitorID string) []store.Check {
	f.t.Helper()
	var checks []store.Check
	f.do(func(tx pgx.Tx) (err error) {
		checks, err = store.RecentChecks(context.Background(), tx, monitorID, 0, 100)
		return
	})
	return checks
}

// With confirmation on, a state that starts to change is settled in the same round: an outage
// becomes an incident, and a recovery closes it, without waiting for further rounds. A single
// blip is checked again and comes to nothing.
func TestConfirmationSettlesWithinOneRound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.sched.Confirm = 10 * time.Millisecond
	var status atomic.Int32
	var blips atomic.Int32
	status.Store(200)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if blips.Add(-1) >= 0 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer site.Close()
	in := input("http") // down after 2 failures, up after 2 successes
	in.URL = &site.URL
	in.AllowPrivateNetwork = true
	m := f.create(in)
	f.tick()
	if got := f.monitor(m.ID); got.Health != "up" || len(f.checks(m.ID)) != 1 {
		t.Fatalf("a healthy site needs one check: %s after %d", got.Health, len(f.checks(m.ID)))
	}

	// One failed answer, then fine again: checked a second time, no incident.
	blips.Store(1)
	f.tick()
	if got := f.monitor(m.ID); got.Health != "up" || len(f.incidents()) != 0 || len(f.checks(m.ID)) != 3 {
		t.Fatalf("a blip: health %s, %d incidents, %d checks (want up, 0, 3)", got.Health, len(f.incidents()), len(f.checks(m.ID)))
	}

	// A real outage: confirmed and opened in one round.
	status.Store(500)
	f.tick()
	if got := f.monitor(m.ID); got.Health != "down" || len(f.incidents()) != 1 || len(f.checks(m.ID)) != 5 {
		t.Fatalf("an outage: health %s, %d incidents, %d checks (want down, 1, 5)", got.Health, len(f.incidents()), len(f.checks(m.ID)))
	}
	// Still down: nothing is changing, so one check a round.
	f.tick()
	if n := len(f.checks(m.ID)); n != 6 {
		t.Fatalf("a settled outage is checked once a round: %d checks", n)
	}

	// And the recovery.
	status.Store(200)
	f.tick()
	got := f.monitor(m.ID)
	if got.Health != "up" || got.OpenIncidentID != nil || f.incidents()[0].Status != "resolved" || len(f.checks(m.ID)) != 8 {
		t.Fatalf("a recovery: %+v, %d checks", got, len(f.checks(m.ID)))
	}
}

// A slow first answer is taken as the service waking up: recorded as a warm-up, followed at once
// by a second check, and left out of the response-time figures.
func TestWarmupIsRecordedApart(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.sched.Warm = 80 * time.Millisecond
	var asleep atomic.Bool
	asleep.Store(true)
	site := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		if asleep.Swap(false) {
			time.Sleep(200 * time.Millisecond)
		}
	}))
	defer site.Close()
	in := input("http")
	in.URL = &site.URL
	in.AllowPrivateNetwork = true
	m := f.create(in)
	f.tick()

	checks := f.checks(m.ID) // newest first
	if len(checks) != 2 || !checks[1].Warmup || checks[1].LatencyMS < 200 || checks[0].Warmup || !checks[0].OK || !checks[1].OK {
		t.Fatalf("want a warm-up then a normal check: %+v", checks)
	}
	var stats map[string]store.Uptime
	f.do(func(tx pgx.Tx) (err error) {
		stats, err = store.UptimeStats(context.Background(), tx, []string{m.ID}, time.Now())
		return
	})
	u := stats[m.ID]
	if u.Checks24h != 2 || u.OK24h != 2 || u.P50 == nil || *u.P50 >= 80 {
		t.Fatalf("both count for uptime, only the second for response time: %+v (p50 %v)", u, u.P50)
	}

	// Awake now: one check a round.
	f.tick()
	if n := len(f.checks(m.ID)); n != 3 {
		t.Fatalf("an awake service is checked once: %d checks", n)
	}
}
