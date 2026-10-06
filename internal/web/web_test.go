package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MrtnOmwenga/lighthouse/internal/auth"
	"github.com/MrtnOmwenga/lighthouse/internal/config"
	"github.com/MrtnOmwenga/lighthouse/internal/monitor"
	"github.com/MrtnOmwenga/lighthouse/internal/site"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
	"github.com/MrtnOmwenga/lighthouse/internal/testdb"
	"github.com/MrtnOmwenga/lighthouse/internal/web"
)

func TestMain(m *testing.M) { testdb.Main(m) }

const ownerGitHubID = 4242

type env struct {
	content *site.Site
	t       *testing.T
	db      testdb.DB
	url     string
	owner   string // owner tenant
	github  *httptest.Server
}

// start runs Lighthouse against a fresh database, with a fake GitHub that knows two users:
// the owner (code "owner") and a stranger (code "stranger").
func start(t *testing.T, tweak func(*config.Config), content ...*site.Site) *env {
	t.Helper()
	return startWith(t, tweak, nil, content...)
}

// startWith is start, with a chance to adjust the server before it serves.
func startWith(t *testing.T, tweak func(*config.Config), adjust func(*web.Server), content ...*site.Site) *env {
	t.Helper()
	db := testdb.New(t)
	e := &env{t: t, db: db}
	if len(content) > 0 {
		e.content = content[0]
	}
	e.github = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = r.ParseForm()
			if r.Form.Get("client_secret") != "shh" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token-" + r.Form.Get("code")})
		case "/user":
			switch r.Header.Get("Authorization") {
			case "Bearer token-owner":
				_ = json.NewEncoder(w).Encode(map[string]any{"id": ownerGitHubID, "login": "MrtnOmwenga"})
			case "Bearer token-stranger":
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "login": "stranger"})
			default:
				w.WriteHeader(http.StatusUnauthorized)
			}
		}
	}))
	t.Cleanup(e.github.Close)

	var handler http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)
	e.url = srv.URL
	cfg, err := config.Load(func(k string) string {
		return map[string]string{
			"DATABASE_URL": db.AppURL, "PUBLIC_URL": srv.URL, "LIGHTHOUSE_ENV": "test", "OWNER_NAME": "Martin",
			"STATUS_CACHE_SECONDS": "0", // tests change data and read it straight back
			"GITHUB_CLIENT_ID":     "client", "GITHUB_CLIENT_SECRET": "shh", "OWNER_GITHUB_ID": fmt.Sprint(ownerGitHubID),
			"GITHUB_OAUTH_BASE": e.github.URL, "GITHUB_API_BASE": e.github.URL,
		}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	if tweak != nil {
		tweak(&cfg)
	}
	if e.owner, err = store.OwnerTenant(context.Background(), db.App, cfg.OwnerName); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv2 := web.New(cfg, db.App, auth.New(db.App, cfg), log, e.owner)
	if e.content != nil {
		srv2.Site = e.content
	}
	if adjust != nil {
		adjust(srv2)
	}
	handler = srv2.Handler()
	return e
}

// browser is a client with its own cookies that sends the Origin a browser on the site would.
type browser struct {
	e      *env
	client *http.Client
}

func (e *env) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{e: e, client: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (b *browser) do(method, path string, body any, origin ...string) (*http.Response, string) {
	b.e.t.Helper()
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = strings.NewReader(string(raw))
	}
	req, _ := http.NewRequest(method, b.e.url+path, r)
	if method != http.MethodGet {
		req.Header.Set("Origin", b.e.url)
		if len(origin) > 0 {
			req.Header.Set("Origin", origin[0])
		}
	}
	resp, err := b.client.Do(req)
	if err != nil {
		b.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, string(raw)
}

func (b *browser) expect(status int, method, path string, body any) string {
	b.e.t.Helper()
	resp, out := b.do(method, path, body)
	if resp.StatusCode != status {
		b.e.t.Fatalf("%s %s: %d, want %d: %s", method, path, resp.StatusCode, status, out)
	}
	return out
}

func decode[T any](t *testing.T, raw string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return v
}

func (e *env) ownerData(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	if err := store.WithTenant(context.Background(), e.db.App, e.owner, fn); err != nil {
		e.t.Fatal(err)
	}
}

func TestStatusPageShowsOnlyPublicData(t *testing.T) {
	t.Parallel()
	e := start(t, nil)
	ctx := context.Background()
	var private store.Incident
	var public store.Incident
	e.ownerData(func(tx pgx.Tx) error {
		mk := func(name string, pub bool) store.MonitorInput {
			return store.MonitorInput{Name: name, Slug: strings.ToLower(name), Kind: "simulated", SimulatedMode: "up", IntervalSeconds: 60,
				TimeoutMS: 1000, ExpectedStatusMin: 200, ExpectedStatusMax: 299, FailureThreshold: 1, RecoveryThreshold: 1, Public: pub}
		}
		if _, err := store.CreateMonitor(ctx, tx, e.owner, mk("GhostChat", true)); err != nil {
			return err
		}
		if _, err := store.CreateMonitor(ctx, tx, e.owner, mk("HiddenAdmin", false)); err != nil {
			return err
		}
		var err error
		if public, err = store.CreateIncident(ctx, tx, e.owner, store.Incident{Title: "Slow messages", Severity: "medium", Public: true, StartedAt: time.Now()}); err != nil {
			return err
		}
		if _, err = store.AddEvent(ctx, tx, e.owner, store.Event{IncidentID: public.ID, At: time.Now(), Kind: "comment", Message: "We're on it.", Public: true, Author: "Martin"}); err != nil {
			return err
		}
		if _, err = store.AddEvent(ctx, tx, e.owner, store.Event{IncidentID: public.ID, At: time.Now(), Kind: "comment", Message: "NOTE-TO-SELF restart worker-3", Public: false, Author: "Martin"}); err != nil {
			return err
		}
		private, err = store.CreateIncident(ctx, tx, e.owner, store.Incident{Title: "HiddenAdmin is down", Severity: "high", StartedAt: time.Now()})
		return err
	})

	visitor := e.browser()
	resp, page := visitor.do("GET", "/status", nil)
	if resp.StatusCode != 200 || !strings.Contains(page, "GhostChat") || !strings.Contains(page, "Slow messages") || !strings.Contains(page, "We&#39;re on it.") {
		t.Fatalf("status page: %d\n%s", resp.StatusCode, page)
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatal("no CSP")
	}
	_, detail := visitor.do("GET", "/status/incidents/"+public.ID, nil)
	_, api := visitor.do("GET", "/api/status", nil)
	for _, body := range []string{page, detail, api} {
		for _, secret := range []string{"HiddenAdmin", "NOTE-TO-SELF"} {
			if strings.Contains(body, secret) {
				t.Errorf("public output leaks %q", secret)
			}
		}
	}
	if resp, _ := visitor.do("GET", "/status/incidents/"+private.ID, nil); resp.StatusCode != 404 {
		t.Fatalf("a private incident's page: %d", resp.StatusCode)
	}
	if resp, _ := visitor.do("GET", "/status/incidents/not-a-uuid", nil); resp.StatusCode != 404 {
		t.Fatalf("a malformed id: %d", resp.StatusCode)
	}
}

func TestGitHubSignIn(t *testing.T) {
	t.Parallel()
	e := start(t, nil)

	signIn := func(b *browser, code string, stateOverride string) *http.Response {
		resp, _ := b.do("GET", "/auth/github", nil)
		loc, _ := url.Parse(resp.Header.Get("Location"))
		if resp.StatusCode != 302 || !strings.HasPrefix(loc.String(), e.github.URL+"/login/oauth/authorize") {
			t.Fatalf("start: %d %s", resp.StatusCode, loc)
		}
		state := loc.Query().Get("state")
		if stateOverride != "" {
			state = stateOverride
		}
		resp, _ = b.do("GET", "/auth/github/callback?"+url.Values{"code": {code}, "state": {state}}.Encode(), nil)
		return resp
	}

	owner := e.browser()
	if resp := signIn(owner, "owner", ""); resp.StatusCode != 302 {
		t.Fatalf("owner sign-in: %d", resp.StatusCode)
	}
	me := decode[map[string]string](t, owner.expect(200, "GET", "/api/me", nil))
	if me["role"] != "owner" || me["login"] != "MrtnOmwenga" {
		t.Fatalf("me: %v", me)
	}

	stranger := e.browser()
	if resp := signIn(stranger, "stranger", ""); resp.StatusCode != 403 {
		t.Fatalf("a stranger's sign-in: %d", resp.StatusCode)
	}
	stranger.expect(401, "GET", "/api/me", nil)

	// A callback whose state doesn't match this browser's (a forged or replayed login link).
	forged := e.browser()
	if resp := signIn(forged, "owner", "attacker-state"); resp.StatusCode != 400 {
		t.Fatalf("state mismatch: %d", resp.StatusCode)
	}
	forged.expect(401, "GET", "/api/me", nil)

	// Logging out ends the session on the server, not just in the browser.
	owner.expect(204, "POST", "/auth/logout", nil)
	owner.expect(401, "GET", "/api/me", nil)
}

func TestDevLogin(t *testing.T) {
	t.Parallel()
	off := start(t, nil)
	off.browser().expect(403, "POST", "/auth/dev", nil)

	on := start(t, func(c *config.Config) { c.DevLogin = true })
	b := on.browser()
	b.expect(200, "POST", "/auth/dev", nil)
	if me := decode[map[string]string](t, b.expect(200, "GET", "/api/me", nil)); me["role"] != "owner" {
		t.Fatalf("me: %v", me)
	}

	// Even if configuration were bypassed, production refuses it.
	prod := start(t, func(c *config.Config) { c.DevLogin = true; c.Env = "production" })
	prod.browser().expect(403, "POST", "/auth/dev", nil)
}

func TestSandbox(t *testing.T) {
	t.Parallel()
	e := start(t, nil)
	ctx := context.Background()
	var ownerMonitor store.Monitor
	e.ownerData(func(tx pgx.Tx) (err error) {
		ownerMonitor, err = store.CreateMonitor(ctx, tx, e.owner, store.MonitorInput{Name: "Private API", Slug: "private-api", Kind: "simulated",
			SimulatedMode: "up", IntervalSeconds: 60, TimeoutMS: 1000, ExpectedStatusMin: 200, ExpectedStatusMax: 299, FailureThreshold: 1, RecoveryThreshold: 1})
		return
	})

	a, b := e.browser(), e.browser()
	a.expect(201, "POST", "/api/sandbox", nil)
	b.expect(201, "POST", "/api/sandbox", nil)
	mine := decode[[]store.Monitor](t, a.expect(200, "GET", "/api/monitors", nil))
	if len(mine) != 3 {
		t.Fatalf("a sandbox starts with the sample monitors: %d", len(mine))
	}
	for _, m := range mine {
		if m.Name == "Private API" {
			t.Fatal("a sandbox sees the owner's monitors")
		}
	}
	// Another tenant's monitor, by id: indistinguishable from a missing one.
	a.expect(404, "GET", "/api/monitors/"+ownerMonitor.ID, nil)
	b.expect(404, "GET", "/api/monitors/"+mine[0].ID, nil)
	b.expect(404, "DELETE", "/api/monitors/"+mine[0].ID, nil)
	b.expect(404, "PUT", "/api/monitors/"+mine[0].ID+"/mode", map[string]string{"mode": "down"})

	// Sandboxes never send real traffic.
	a.expect(422, "POST", "/api/monitors", map[string]any{"name": "Probe", "kind": "http", "url": "http://169.254.169.254/"})
	a.expect(403, "POST", "/api/monitors", map[string]any{"name": "Probe", "kind": "simulated", "allowPrivateNetwork": true})

	// Break a sample site, and add monitors up to the limit.
	a.expect(200, "PUT", "/api/monitors/"+mine[0].ID+"/mode", map[string]string{"mode": "down"})
	a.expect(409, "POST", "/api/monitors", map[string]any{"name": "Storefront", "kind": "simulated"})
	for i := range 7 {
		a.expect(201, "POST", "/api/monitors", map[string]any{"name": fmt.Sprintf("Site %d", i), "kind": "simulated"})
	}
	a.expect(422, "POST", "/api/monitors", map[string]any{"name": "One too many", "kind": "simulated"})
}

func TestSandboxCreationIsRateLimited(t *testing.T) {
	t.Parallel()
	e := start(t, nil)
	for range 3 {
		e.browser().expect(201, "POST", "/api/sandbox", nil)
	}
	e.browser().expect(429, "POST", "/api/sandbox", nil)
}

func TestCrossSiteWritesAreRefused(t *testing.T) {
	t.Parallel()
	e := start(t, nil)
	b := e.browser()
	b.expect(201, "POST", "/api/sandbox", nil)
	resp, body := b.do("POST", "/api/monitors", map[string]any{"name": "x", "kind": "simulated"}, "https://evil.example")
	if resp.StatusCode != 403 || resp.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("cross-site POST: %d %s", resp.StatusCode, body)
	}
	resp, _ = b.do("POST", "/auth/logout", nil, "http://127.0.0.1.evil.example")
	if resp.StatusCode != 403 {
		t.Fatalf("look-alike origin: %d", resp.StatusCode)
	}
}

func TestAPIRequiresASession(t *testing.T) {
	t.Parallel()
	e := start(t, nil)
	anon := e.browser()
	for _, path := range []string{"/api/monitors", "/api/incidents", "/api/my-status", "/api/me"} {
		resp, body := anon.do("GET", path, nil)
		if resp.StatusCode != 401 || !strings.Contains(body, `"title"`) {
			t.Errorf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
	// A forged cookie is just no session.
	u, _ := url.Parse(e.url)
	anon.client.Jar.SetCookies(u, []*http.Cookie{{Name: auth.SessionCookie, Value: "forged"}})
	anon.expect(401, "GET", "/api/monitors", nil)
}

func TestIncidentWorkflow(t *testing.T) {
	t.Parallel()
	e := start(t, func(c *config.Config) { c.DevLogin = true })
	b := e.browser()
	b.expect(200, "POST", "/auth/dev", nil)

	type detail struct {
		store.Incident
		Events []store.Event `json:"events"`
	}
	inc := decode[detail](t, b.expect(201, "POST", "/api/incidents", map[string]any{
		"title": "Database maintenance", "severity": "low", "public": true, "message": "Planned upgrade to Postgres 18.",
	}))
	b.expect(201, "POST", "/api/incidents/"+inc.ID+"/comments", map[string]any{"message": "internal: snapshot taken", "public": false})
	b.expect(201, "POST", "/api/incidents/"+inc.ID+"/comments", map[string]any{"message": "Upgrade under way.", "public": true})
	b.expect(200, "PATCH", "/api/incidents/"+inc.ID, map[string]any{"status": "resolved", "severity": "medium"})
	b.expect(422, "PATCH", "/api/incidents/"+inc.ID, map[string]any{"status": "exploded"})
	b.expect(422, "POST", "/api/incidents", map[string]any{"title": "x", "message": "y", "extra": true})

	got := decode[detail](t, b.expect(200, "GET", "/api/incidents/"+inc.ID, nil))
	var kinds []string
	for _, ev := range got.Events {
		kinds = append(kinds, ev.Kind)
	}
	if got.Status != "resolved" || got.ResolvedAt == nil || got.Severity != "medium" ||
		strings.Join(kinds, ",") != "opened,comment,comment,status,severity" {
		t.Fatalf("incident: %+v, events %v", got.Incident, kinds)
	}

	_, page := e.browser().do("GET", "/status/incidents/"+inc.ID, nil)
	if !strings.Contains(page, "Upgrade under way.") || strings.Contains(page, "snapshot taken") {
		t.Fatalf("public timeline:\n%s", page)
	}
}

func TestIncidentPagination(t *testing.T) {
	t.Parallel()
	e := start(t, func(c *config.Config) { c.DevLogin = true })
	base := time.Now().Add(-time.Hour)
	e.ownerData(func(tx pgx.Tx) error {
		for i := range 60 {
			// Pairs share a start time, so the id tiebreak matters.
			if _, err := store.CreateIncident(context.Background(), tx, e.owner, store.Incident{
				Title: fmt.Sprintf("Incident %d", i), Severity: "low", StartedAt: base.Add(time.Duration(i/2) * time.Second),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	b := e.browser()
	b.expect(200, "POST", "/auth/dev", nil)
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		page := decode[struct {
			Incidents []store.Incident `json:"incidents"`
			Next      string           `json:"next"`
		}](t, b.expect(200, "GET", "/api/incidents?cursor="+url.QueryEscape(cursor), nil))
		pages++
		for _, i := range page.Incidents {
			if seen[i.ID] {
				t.Fatalf("incident %s on two pages", i.Title)
			}
			seen[i.ID] = true
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != 60 || pages != 3 {
		t.Fatalf("saw %d incidents over %d pages", len(seen), pages)
	}
	b.expect(422, "GET", "/api/incidents?cursor=garbage!", nil)
}

// With checks scheduled from outside, nothing runs between rounds: a sandbox is checked by its own
// console reads, so a visitor can break a site and watch the incident open.
func TestConsoleReadsDriveTheSandbox(t *testing.T) {
	t.Parallel()
	e := startWith(t, nil, func(s *web.Server) {
		sched := &monitor.Scheduler{Pool: s.Pool, Prober: monitor.NewProber(), Workers: 4, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		s.Drive = sched.RunTenant
	})
	ctx := context.Background()
	b := e.browser()
	b.expect(201, "POST", "/api/sandbox", nil)

	monitors := decode[[]store.Monitor](t, b.expect(200, "GET", "/api/monitors", nil))
	var checkout store.Monitor
	for _, m := range monitors {
		if m.LastCheckedAt == nil {
			t.Fatalf("%s wasn't checked by the read that listed it", m.Name)
		}
		if m.Name == "Checkout API" {
			checkout = m
		}
	}

	b.expect(200, "PUT", "/api/monitors/"+checkout.ID+"/mode", map[string]string{"mode": "down"})
	for range checkout.FailureThreshold {
		// Each read finds the monitor due again (its interval has passed) and checks it.
		if _, err := e.db.Owner.Exec(ctx, "UPDATE monitors SET next_check_at = now() WHERE id = $1", checkout.ID); err != nil {
			t.Fatal(err)
		}
		b.expect(200, "GET", "/api/monitors", nil)
	}
	got := decode[store.Monitor](t, b.expect(200, "GET", "/api/monitors/"+checkout.ID, nil))
	if got.Health != "down" || got.OpenIncidentID == nil {
		t.Fatalf("after %d failed checks: health %s, incident %v", checkout.FailureThreshold, got.Health, got.OpenIncidentID)
	}
}
