package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MrtnOmwenga/lighthouse/internal/config"
	"github.com/MrtnOmwenga/lighthouse/internal/monitor"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
	"github.com/MrtnOmwenga/lighthouse/internal/web"
)

func withChecks(s *web.Server) {
	s.Checks = &monitor.Scheduler{Pool: s.Pool, Prober: monitor.NewProber(), Workers: 4, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

type problemBody struct {
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	RequestID string `json:"requestId"`
	Errors    []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"errors"`
}

// An invalid monitor is refused with every problem, each against its field.
func TestValidationListsEveryField(t *testing.T) {
	t.Parallel()
	e := start(t, func(c *config.Config) { c.DevLogin = true })
	b := e.browser()
	b.expect(200, "POST", "/auth/dev", nil)
	p := decode[problemBody](t, b.expect(422, "POST", "/api/monitors", map[string]any{"name": "", "kind": "http", "intervalSeconds": 2, "timeoutMs": 5}))
	got := map[string]bool{}
	for _, f := range p.Errors {
		got[f.Field] = true
	}
	for _, field := range []string{"name", "slug", "url", "intervalSeconds", "timeoutMs"} {
		if !got[field] {
			t.Errorf("no error reported for %s: %+v", field, p.Errors)
		}
	}
	if len(p.Errors) != 5 || p.Detail == "" {
		t.Errorf("want exactly those five, and a one-line detail: %+v", p)
	}

	// A taken slug points at the slug field too.
	b.expect(201, "POST", "/api/monitors", map[string]any{"name": "Shop", "kind": "simulated"})
	p = decode[problemBody](t, b.expect(409, "POST", "/api/monitors", map[string]any{"name": "Shop", "kind": "simulated"}))
	if len(p.Errors) != 1 || p.Errors[0].Field != "slug" {
		t.Errorf("duplicate slug: %+v", p)
	}
}

// Every response names its request; an unexpected failure repeats the name in its body, and says
// nothing else about what went wrong.
func TestFailuresCanBeTraced(t *testing.T) {
	t.Parallel()
	e := start(t, nil)
	b := e.browser()
	resp, _ := b.do("GET", "/privacy", nil)
	first := resp.Header.Get("X-Request-Id")
	if len(first) != 16 {
		t.Fatalf("X-Request-Id = %q", first)
	}

	e.db.App.Close()
	resp, body := b.do("GET", "/api/status", nil)
	p := decode[problemBody](t, body)
	id := resp.Header.Get("X-Request-Id")
	if resp.StatusCode != 500 || p.RequestID == "" || p.RequestID != id || id == first {
		t.Fatalf("a failure should carry its own request id: %d header %q body %+v", resp.StatusCode, id, p)
	}
	if p.Title != "Something went wrong" || p.Detail != "" {
		t.Fatalf("a failure must not describe itself to the visitor: %+v", p)
	}
}

// "Check now" records a real check at once; "Test" tries settings and records nothing.
func TestCheckNowAndTest(t *testing.T) {
	t.Parallel()
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/broken" {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer site.Close()
	e := startWith(t, func(c *config.Config) { c.DevLogin = true }, withChecks)
	b := e.browser()
	b.expect(200, "POST", "/auth/dev", nil)

	type result struct {
		OK      bool   `json:"ok"`
		Failure string `json:"failure"`
	}
	settings := func(path string, private bool) map[string]any {
		return map[string]any{"name": "Site", "kind": "http", "url": site.URL + path, "allowPrivateNetwork": private}
	}
	if r := decode[result](t, b.expect(200, "POST", "/api/monitors/test", settings("/", true))); !r.OK {
		t.Fatalf("testing a healthy site: %+v", r)
	}
	if r := decode[result](t, b.expect(200, "POST", "/api/monitors/test", settings("/broken", true))); r.OK || r.Failure != "status" {
		t.Fatalf("testing a failing site: %+v", r)
	}
	// The address guard applies to a test exactly as to a scheduled check.
	if r := decode[result](t, b.expect(200, "POST", "/api/monitors/test", settings("/", false))); r.OK || r.Failure != "blocked" {
		t.Fatalf("testing a private address without permission: %+v", r)
	}
	b.expect(422, "POST", "/api/monitors/test", map[string]any{"kind": "http", "url": "not a url"})
	var checks int
	if err := e.db.Owner.QueryRow(context.Background(), "SELECT count(*) FROM checks").Scan(&checks); err != nil || checks != 0 {
		t.Fatalf("a test must record nothing: %d checks, %v", checks, err)
	}

	m := decode[store.Monitor](t, b.expect(201, "POST", "/api/monitors", settings("/", true)))
	got := decode[struct {
		Monitor store.Monitor `json:"monitor"`
		Check   store.Check   `json:"check"`
	}](t, b.expect(200, "POST", "/api/monitors/"+m.ID+"/check", nil))
	if !got.Check.OK || got.Monitor.LastCheckedAt == nil || got.Monitor.Health != "up" {
		t.Fatalf("check now: %+v", got)
	}

	// A paused monitor isn't checked; someone else's doesn't exist.
	paused := settings("/", true)
	paused["paused"] = true
	b.expect(200, "PUT", "/api/monitors/"+m.ID, paused)
	b.expect(409, "POST", "/api/monitors/"+m.ID+"/check", nil)
	visitor := e.browser()
	visitor.expect(201, "POST", "/api/sandbox", nil)
	visitor.expect(404, "POST", "/api/monitors/"+m.ID+"/check", nil)
	visitor.expect(422, "POST", "/api/monitors/test", settings("/", false))
}

// Starting a sandbox twice returns to the first, and a sandbox can be put back to how it started.
func TestSandboxIsResumedAndReset(t *testing.T) {
	t.Parallel()
	e := startWith(t, func(c *config.Config) { c.DevLogin = true }, withChecks)
	ctx := context.Background()
	b := e.browser()
	b.expect(201, "POST", "/api/sandbox", nil)
	if !decode[map[string]any](t, b.expect(200, "POST", "/api/sandbox", nil))["resumed"].(bool) {
		t.Fatal("a second start should resume the first sandbox")
	}
	var sandboxes int
	if err := e.db.Owner.QueryRow(ctx, "SELECT count(*) FROM tenants WHERE kind = 'sandbox'").Scan(&sandboxes); err != nil || sandboxes != 1 {
		t.Fatalf("%d sandbox tenants, want 1 (%v)", sandboxes, err)
	}

	// Make a mess: an incident, a deleted monitor, an extra one.
	monitors := decode[[]store.Monitor](t, b.expect(200, "GET", "/api/monitors", nil))
	b.expect(200, "PUT", "/api/monitors/"+monitors[0].ID+"/mode", map[string]string{"mode": "down"})
	for range 2 {
		b.expect(200, "POST", "/api/monitors/"+monitors[0].ID+"/check", nil)
	}
	b.expect(204, "DELETE", "/api/monitors/"+monitors[1].ID, nil)
	b.expect(201, "POST", "/api/monitors", map[string]any{"name": "Extra", "kind": "simulated", "intervalSeconds": 30})
	if n := len(decode[struct{ Incidents []any }](t, b.expect(200, "GET", "/api/incidents", nil)).Incidents); n != 1 {
		t.Fatalf("%d incidents before the reset, want 1", n)
	}

	b.expect(204, "POST", "/api/sandbox/reset", nil)
	after := decode[[]store.Monitor](t, b.expect(200, "GET", "/api/monitors", nil))
	names := map[string]bool{}
	for _, m := range after {
		names[m.Name] = true
		if m.Health != "unknown" || m.OpenIncidentID != nil {
			t.Errorf("%s isn't fresh after the reset: %+v", m.Name, m)
		}
	}
	if len(after) != 3 || !names["Storefront"] || !names["Checkout API"] || !names["Search"] {
		t.Fatalf("after the reset: %v", names)
	}
	if n := len(decode[struct{ Incidents []any }](t, b.expect(200, "GET", "/api/incidents", nil)).Incidents); n != 0 {
		t.Fatalf("%d incidents after the reset", n)
	}

	// The owner's data can't be reset this way.
	owner := e.browser()
	owner.expect(200, "POST", "/auth/dev", nil)
	owner.expect(403, "POST", "/api/sandbox/reset", nil)
}

// The monitors screen gets its monitors and their figures in one answer, private ones included.
func TestOverview(t *testing.T) {
	t.Parallel()
	e := startWith(t, func(c *config.Config) { c.DevLogin = true }, withChecks)
	b := e.browser()
	b.expect(200, "POST", "/auth/dev", nil)
	m := decode[store.Monitor](t, b.expect(201, "POST", "/api/monitors", map[string]any{"name": "Internal", "kind": "simulated", "public": false}))
	b.expect(200, "POST", "/api/monitors/"+m.ID+"/check", nil)
	got := decode[struct {
		Monitors []store.Monitor `json:"monitors"`
		Stats    []struct {
			Slug      string   `json:"slug"`
			Uptime24h *float64 `json:"uptime24h"`
		} `json:"stats"`
	}](t, b.expect(200, "GET", "/api/overview", nil))
	if len(got.Monitors) != 1 || len(got.Stats) != 1 || got.Stats[0].Slug != "internal" || got.Stats[0].Uptime24h == nil || *got.Stats[0].Uptime24h != 100 {
		t.Fatalf("overview: %+v", got)
	}
	e.browser().expect(401, "GET", "/api/overview", nil)
}
