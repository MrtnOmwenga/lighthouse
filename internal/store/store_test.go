package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/MrtnOmwenga/lighthouse/internal/store"
	"github.com/MrtnOmwenga/lighthouse/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

func sandbox(t *testing.T, db testdb.DB) string {
	t.Helper()
	id := uuid.NewString()
	if err := store.CreateSandbox(context.Background(), db.App, id, func(pgx.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	return id
}

func simulated(slug string) store.MonitorInput {
	return store.MonitorInput{
		Name: "Site " + slug, Slug: slug, Kind: "simulated", SimulatedMode: "up", IntervalSeconds: 60, TimeoutMS: 5000,
		ExpectedStatusMin: 200, ExpectedStatusMax: 399, FailureThreshold: 2, RecoveryThreshold: 2, Public: true,
	}
}

func in(t *testing.T, db testdb.DB, tenant string, fn func(pgx.Tx) error) error {
	t.Helper()
	return store.WithTenant(context.Background(), db.App, tenant, fn)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// One tenant can't see, change or attach anything to another tenant's rows, whatever the query.
func TestTenantsAreIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testdb.New(t)
	a, b := sandbox(t, db), sandbox(t, db)

	var mine store.Monitor
	var incident store.Incident
	must(t, in(t, db, a, func(tx pgx.Tx) (err error) {
		if mine, err = store.CreateMonitor(ctx, tx, a, simulated("web")); err != nil {
			return err
		}
		incident, err = store.CreateIncident(ctx, tx, a, store.Incident{Title: "Outage", Severity: "high", StartedAt: time.Now()})
		return err
	}))

	must(t, in(t, db, b, func(tx pgx.Tx) error {
		if list, err := store.ListMonitors(ctx, tx, false); err != nil || len(list) != 0 {
			t.Errorf("B lists A's monitors: %v %v", list, err)
		}
		if _, err := store.GetMonitor(ctx, tx, mine.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("B reads A's monitor: %v", err)
		}
		if _, err := store.UpdateMonitor(ctx, tx, mine.ID, simulated("stolen")); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("B updates A's monitor: %v", err)
		}
		if err := store.DeleteMonitor(ctx, tx, mine.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("B deletes A's monitor: %v", err)
		}
		if _, err := store.GetIncident(ctx, tx, incident.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("B reads A's incident: %v", err)
		}
		if list, err := store.ListIncidents(ctx, tx, store.IncidentFilter{Limit: 50}); err != nil || len(list) != 0 {
			t.Errorf("B lists A's incidents: %v %v", list, err)
		}
		return nil
	}))

	// Writing into A under B's tenant id, or naming A's rows from B's: refused by the policy's
	// WITH CHECK or by the composite (tenant_id, id) foreign keys.
	attempts := map[string]func(tx pgx.Tx) error{
		"monitor in A's name": func(tx pgx.Tx) error { _, err := store.CreateMonitor(ctx, tx, a, simulated("x")); return err },
		"check on A's monitor": func(tx pgx.Tx) error {
			_, err := store.InsertCheck(ctx, tx, b, store.Check{MonitorID: mine.ID, At: time.Now(), OK: true})
			return err
		},
		"comment on A's incident": func(tx pgx.Tx) error {
			_, err := store.AddEvent(ctx, tx, b, store.Event{IncidentID: incident.ID, At: time.Now(), Kind: "comment", Message: "hi", Author: "b"})
			return err
		},
		"incident on A's monitor": func(tx pgx.Tx) error {
			_, err := store.CreateIncident(ctx, tx, b, store.Incident{MonitorID: &mine.ID, Title: "x", Severity: "low", StartedAt: time.Now()})
			return err
		},
	}
	for name, attempt := range attempts {
		if err := in(t, db, b, attempt); err == nil {
			t.Errorf("%s: allowed", name)
		}
	}

	// A's data is untouched.
	must(t, in(t, db, a, func(tx pgx.Tx) error {
		m, err := store.GetMonitor(ctx, tx, mine.ID)
		if err != nil || m.Slug != "web" {
			t.Errorf("A's monitor changed: %+v %v", m, err)
		}
		return nil
	}))
}

// Without a tenant set, the API role sees nothing: forgetting WithTenant fails closed.
func TestNoTenantSeesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testdb.New(t)
	a := sandbox(t, db)
	must(t, in(t, db, a, func(tx pgx.Tx) error { _, err := store.CreateMonitor(ctx, tx, a, simulated("web")); return err }))

	for _, table := range []string{"tenants", "monitors", "checks", "incidents", "incident_events", "sessions"} {
		var n int
		must(t, db.App.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
		if n != 0 {
			t.Errorf("%s: %d rows visible without a tenant", table, n)
		}
	}
	var owner int
	must(t, db.Owner.QueryRow(ctx, "SELECT count(*) FROM monitors").Scan(&owner))
	if owner != 1 {
		t.Fatalf("control: the superuser should see the monitor, saw %d", owner)
	}
}

// The API role can't switch the protection off.
func TestAppRoleCannotBypassRLS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testdb.New(t)
	for _, stmt := range []string{
		"ALTER TABLE monitors DISABLE ROW LEVEL SECURITY",
		"ALTER TABLE monitors NO FORCE ROW LEVEL SECURITY",
		"DROP POLICY tenant_isolation ON monitors",
		"SET row_security = off; SELECT * FROM monitors",
		"DELETE FROM checks",
		"UPDATE incident_events SET message = 'rewritten'",
	} {
		if _, err := db.App.Exec(ctx, stmt); err == nil {
			t.Errorf("allowed: %s", stmt)
		}
	}
}

// The definer functions answer across tenants, but only what they must.
func TestCrossTenantFunctions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testdb.New(t)
	a, b := sandbox(t, db), sandbox(t, db)
	var ma, mb store.Monitor
	must(t, in(t, db, a, func(tx pgx.Tx) (err error) { ma, err = store.CreateMonitor(ctx, tx, a, simulated("a")); return }))
	must(t, in(t, db, b, func(tx pgx.Tx) (err error) { mb, err = store.CreateMonitor(ctx, tx, b, simulated("b")); return }))

	due, err := store.DueMonitors(ctx, db.App, 10, 0)
	must(t, err)
	got := map[string]string{}
	for _, d := range due {
		got[d.MonitorID] = d.TenantID
	}
	if got[ma.ID] != a || got[mb.ID] != b || len(got) != 2 {
		t.Fatalf("due monitors = %v", got)
	}

	// Claiming is atomic: the second claim of the same due monitor finds nothing.
	must(t, in(t, db, a, func(tx pgx.Tx) error { _, err := store.ClaimMonitor(ctx, tx, ma.ID, 0); return err }))
	if err := in(t, db, a, func(tx pgx.Tx) error { _, err := store.ClaimMonitor(ctx, tx, ma.ID, 0); return err }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second claim: %v", err)
	}
	if due, _ := store.DueMonitors(ctx, db.App, 10, 0); len(due) != 1 || due[0].MonitorID != mb.ID {
		t.Fatalf("a claimed monitor isn't due any more: %v", due)
	}

	// Sessions resolve to their tenant.
	must(t, in(t, db, a, func(tx pgx.Tx) error {
		return store.CreateSession(ctx, tx, "hash-a", store.Session{TenantID: a, Role: "sandbox", ExpiresAt: time.Now().Add(time.Hour)})
	}))
	s, err := store.LookupSession(ctx, db.App, "hash-a")
	if err != nil || s.TenantID != a || s.Role != "sandbox" {
		t.Fatalf("session = %+v %v", s, err)
	}
	if _, err := store.LookupSession(ctx, db.App, "unknown"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown session: %v", err)
	}

	// One owner tenant, however often it's asked for.
	o1, err := store.OwnerTenant(ctx, db.App, "Martin")
	must(t, err)
	o2, err := store.OwnerTenant(ctx, db.App, "Someone else")
	must(t, err)
	if o1 != o2 {
		t.Fatalf("two owner tenants: %s %s", o1, o2)
	}
}

func TestPrune(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testdb.New(t)
	old, fresh := sandbox(t, db), sandbox(t, db)
	var m store.Monitor
	must(t, in(t, db, fresh, func(tx pgx.Tx) (err error) {
		if m, err = store.CreateMonitor(ctx, tx, fresh, simulated("web")); err != nil {
			return err
		}
		for _, age := range []time.Duration{0, 100 * 24 * time.Hour} {
			if _, err = store.InsertCheck(ctx, tx, fresh, store.Check{MonitorID: m.ID, At: time.Now().Add(-age), OK: true}); err != nil {
				return err
			}
		}
		if err := store.CreateSession(ctx, tx, "expired", store.Session{TenantID: fresh, Role: "sandbox", ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
			return err
		}
		return store.CreateSession(ctx, tx, "live", store.Session{TenantID: fresh, Role: "sandbox", ExpiresAt: time.Now().Add(time.Hour)})
	}))
	_, err := db.Owner.Exec(ctx, "UPDATE tenants SET created_at = now() - interval '2 hours' WHERE id = $1", old)
	must(t, err)

	p, err := store.Prune(ctx, db.App, 90*24*time.Hour, time.Hour)
	must(t, err)
	if p != (store.Pruned{Checks: 1, Sandboxes: 1, Sessions: 1}) {
		t.Fatalf("pruned %+v", p)
	}
	if _, err := store.LookupSession(ctx, db.App, "live"); err != nil {
		t.Fatalf("the live session went: %v", err)
	}
}

// Deleting a monitor keeps its incidents (history) but detaches them.
func TestDeletingAMonitorKeepsItsIncidents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testdb.New(t)
	a := sandbox(t, db)
	var inc store.Incident
	must(t, in(t, db, a, func(tx pgx.Tx) error {
		m, err := store.CreateMonitor(ctx, tx, a, simulated("web"))
		if err != nil {
			return err
		}
		if inc, err = store.CreateIncident(ctx, tx, a, store.Incident{MonitorID: &m.ID, Title: "Down", Severity: "high", StartedAt: time.Now()}); err != nil {
			return err
		}
		return store.DeleteMonitor(ctx, tx, m.ID)
	}))
	must(t, in(t, db, a, func(tx pgx.Tx) error {
		got, err := store.GetIncident(ctx, tx, inc.ID)
		if err != nil || got.MonitorID != nil {
			t.Errorf("incident after its monitor was deleted: %+v %v", got, err)
		}
		return nil
	}))
}

// A schedule driven from outside arrives at fixed times: slack lets it take a monitor that is a
// moment short of due, but never by more than a tenth of the monitor's interval.
func TestDueWithSlack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testdb.New(t)
	a := sandbox(t, db)
	slow, fast := simulated("slow"), simulated("fast")
	slow.IntervalSeconds, fast.IntervalSeconds = 900, 10
	var ms, mf store.Monitor
	must(t, in(t, db, a, func(tx pgx.Tx) (err error) { ms, err = store.CreateMonitor(ctx, tx, a, slow); return }))
	must(t, in(t, db, a, func(tx pgx.Tx) (err error) { mf, err = store.CreateMonitor(ctx, tx, a, fast); return }))
	// Both become due in five seconds.
	if _, err := db.Owner.Exec(ctx, "UPDATE monitors SET next_check_at = now() + interval '5 seconds'"); err != nil {
		t.Fatal(err)
	}

	if due, err := store.DueMonitors(ctx, db.App, 10, 0); err != nil || len(due) != 0 {
		t.Fatalf("without slack nothing is due yet: %v %v", due, err)
	}
	due, err := store.DueMonitors(ctx, db.App, 10, time.Minute)
	must(t, err)
	if len(due) != 1 || due[0].MonitorID != ms.ID {
		t.Fatalf("with a minute's slack: want only the 15-minute monitor (the 10-second one may be taken 1 s early), got %v", due)
	}

	claim := func(id string, slack time.Duration) error {
		return in(t, db, a, func(tx pgx.Tx) error { _, err := store.ClaimMonitor(ctx, tx, id, slack); return err })
	}
	if err := claim(ms.ID, 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("claiming early without slack: %v", err)
	}
	if err := claim(mf.ID, time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("claiming a 10-second monitor 5 s early: %v", err)
	}
	must(t, claim(ms.ID, time.Minute))
	if err := claim(ms.ID, time.Minute); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a claimed monitor is a full interval away, beyond any slack: %v", err)
	}
}

// Every migration can be undone, and applied again afterwards: all the way down, and back up.
func TestMigrationsRunDownAndUpAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := testdb.New(t)
	owner := stdlib.OpenDBFromPool(db.Owner)
	tables := func() int {
		var n int
		if err := db.Owner.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'goose_db_version'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := tables()
	if before == 0 {
		t.Fatal("the test database should start migrated")
	}
	must(t, store.MigrateTo(ctx, owner, 0))
	if n := tables(); n != 0 {
		t.Fatalf("after migrating all the way down, %d tables are left", n)
	}
	must(t, store.MigrateTo(ctx, owner, -1))
	if n := tables(); n != before {
		t.Fatalf("after migrating up again: %d tables, want %d", n, before)
	}
	// And it works: a tenant can be created and its monitor found due, as the application role.
	a := sandbox(t, db)
	must(t, in(t, db, a, func(tx pgx.Tx) error { _, err := store.CreateMonitor(ctx, tx, a, simulated("again")); return err }))
	if due, err := store.DueMonitors(ctx, db.App, 10, 0); err != nil || len(due) != 1 {
		t.Fatalf("after the round trip: %v %v", due, err)
	}
}
