package status_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MrtnOmwenga/lighthouse/internal/status"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
	"github.com/MrtnOmwenga/lighthouse/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

func TestBuildShowsOnlyPublicData(t *testing.T) {
	ctx := context.Background()
	db := testdb.New(t)
	owner, err := store.OwnerTenant(ctx, db.App, "Martin")
	if err != nil {
		t.Fatal(err)
	}
	// Noon, so that "an hour ago" is still today whatever time the test runs: the last assertion is
	// about today's bar, and days are counted in UTC.
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	input := func(name string, public bool) store.MonitorInput {
		return store.MonitorInput{Name: name, Slug: strings.ToLower(name), Kind: "simulated", SimulatedMode: "up", IntervalSeconds: 60,
			TimeoutMS: 1000, ExpectedStatusMin: 200, ExpectedStatusMax: 299, FailureThreshold: 1, RecoveryThreshold: 1, Public: public}
	}
	err = store.WithTenant(ctx, db.App, owner, func(tx pgx.Tx) error {
		pub, err := store.CreateMonitor(ctx, tx, owner, input("GhostChat", true))
		if err != nil {
			return err
		}
		priv, err := store.CreateMonitor(ctx, tx, owner, input("SecretAdmin", false))
		if err != nil {
			return err
		}
		// GhostChat: 3 of 4 checks passed in the last day, plus one older failure 3 days ago.
		failed := "status"
		for i, ok := range []bool{true, true, true, false} {
			c := store.Check{MonitorID: pub.ID, At: now.Add(-time.Duration(i+1) * time.Hour), OK: ok, LatencyMS: 100 * (i + 1)}
			if !ok {
				c.Failure = &failed
			}
			if _, err := store.InsertCheck(ctx, tx, owner, c); err != nil {
				return err
			}
		}
		if _, err := store.InsertCheck(ctx, tx, owner, store.Check{MonitorID: pub.ID, At: now.Add(-72 * time.Hour), Failure: &failed}); err != nil {
			return err
		}
		if _, err := store.InsertCheck(ctx, tx, owner, store.Check{MonitorID: priv.ID, At: now.Add(-time.Hour), OK: true}); err != nil {
			return err
		}

		open, err := store.CreateIncident(ctx, tx, owner, store.Incident{Title: "Slow chat delivery", Severity: "medium", Public: true, StartedAt: now.Add(-time.Hour)})
		if err != nil {
			return err
		}
		for _, e := range []store.Event{
			{IncidentID: open.ID, At: now, Kind: "comment", Message: "Investigating slow delivery.", Public: true, Author: "Martin"},
			{IncidentID: open.ID, At: now, Kind: "comment", Message: "INTERNAL: db host 10.0.0.12 is swapping", Public: false, Author: "Martin"},
		} {
			if _, err := store.AddEvent(ctx, tx, owner, e); err != nil {
				return err
			}
		}
		_, err = store.CreateIncident(ctx, tx, owner, store.Incident{MonitorID: &priv.ID, Title: "SecretAdmin is down", Severity: "high", Automatic: true, StartedAt: now})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	page, err := status.Build(ctx, db.App, owner, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(page)
	for _, secret := range []string{"SecretAdmin", "INTERNAL", "10.0.0.12"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the public status leaks %q: %s", secret, raw)
		}
	}
	if len(page.Monitors) != 1 || len(page.Active) != 1 || len(page.Active[0].Events) != 1 {
		t.Fatalf("page: %s", raw)
	}
	m := page.Monitors[0]
	if *m.Uptime24h != 75 || *m.Uptime7d != 60 || *m.P50 != 200 || m.Days[status.Days-1].Uptime == nil {
		t.Fatalf("GhostChat numbers: 24h %v, 7d %v, p50 %v", *m.Uptime24h, *m.Uptime7d, *m.P50)
	}
	if page.Overall != status.Degraded {
		t.Fatalf("an open public incident degrades the page: %s", page.Overall)
	}

	// Other tenants' data never appears either.
	if page, err := status.Build(ctx, db.App, "00000000-0000-0000-0000-000000000000", now); err != nil || len(page.Monitors) != 0 || page.Overall != status.NoMonitorsYet {
		t.Fatalf("an unknown tenant sees %+v %v", page, err)
	}
}
