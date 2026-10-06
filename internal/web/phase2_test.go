package web_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MrtnOmwenga/lighthouse/internal/config"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
	"github.com/MrtnOmwenga/lighthouse/internal/web"
)

func publicMonitor(slug string) store.MonitorInput {
	return store.MonitorInput{Name: slug, Slug: slug, Kind: "simulated", SimulatedMode: "up", IntervalSeconds: 60, TimeoutMS: 1000,
		ExpectedStatusMin: 200, ExpectedStatusMax: 299, FailureThreshold: 1, RecoveryThreshold: 1, Public: true}
}

// The public status is built once and reused for a while, and when the database can't be reached
// the last one built is served, marked as such, instead of an error page.
func TestStatusIsReusedAndSurvivesTheDatabase(t *testing.T) {
	t.Parallel()
	var clock atomic.Int64
	clock.Store(time.Now().Unix())
	e := startWith(t, func(c *config.Config) { c.StatusCache = time.Minute }, func(s *web.Server) {
		s.Now = func() time.Time { return time.Unix(clock.Load(), 0) }
	})
	b := e.browser()
	monitors := func() int {
		return len(decode[struct{ Monitors []any }](t, b.expect(200, "GET", "/api/status", nil)).Monitors)
	}
	add := func(slug string) {
		e.ownerData(func(tx pgx.Tx) error {
			_, err := store.CreateMonitor(context.Background(), tx, e.owner, publicMonitor(slug))
			return err
		})
	}

	add("one")
	if n := monitors(); n != 1 {
		t.Fatalf("first build: %d monitors", n)
	}
	add("two")
	if n := monitors(); n != 1 {
		t.Fatalf("within the minute the status is reused, not rebuilt: %d monitors", n)
	}
	clock.Add(120)
	if n := monitors(); n != 2 {
		t.Fatalf("after the minute it is rebuilt: %d monitors", n)
	}

	// The database goes away.
	clock.Add(120)
	e.db.App.Close()
	body := b.expect(200, "GET", "/status", nil)
	if !strings.Contains(body, "couldn't be refreshed just now") || !strings.Contains(body, "two") {
		t.Fatal("the status page should show the last status built, and say so")
	}
	if n := monitors(); n != 2 {
		t.Fatalf("the API serves the last status too: %d monitors", n)
	}
	b.expect(200, "GET", "/", nil)
}

// With nothing ever built, there is nothing to fall back on.
func TestStatusWithoutADatabaseOrAnEarlierBuild(t *testing.T) {
	t.Parallel()
	e := start(t, nil)
	e.db.App.Close()
	b := e.browser()
	b.expect(503, "GET", "/status", nil)
	b.expect(200, "GET", "/", nil) // the reading matter doesn't need the database
}

// The status page refreshes itself in place, so an open tab isn't a new visit every minute.
func TestStatusPageRefreshesWithoutReloading(t *testing.T) {
	t.Parallel()
	e := start(t, nil)
	body := e.browser().expect(200, "GET", "/status", nil)
	if !strings.Contains(body, `<noscript><meta http-equiv="refresh" content="60"></noscript>`) {
		t.Error("the reload should be for browsers without JavaScript only")
	}
	if !strings.Contains(body, `src="/static/status.js"`) || !strings.Contains(body, `<main id="main" data-refresh="60">`) {
		t.Error("the page should refresh its main part with status.js")
	}
	e.browser().expect(200, "GET", "/static/status.js", nil)
}

// The owner marks a browser as his; from then on it isn't counted, signed in or not.
func TestTheOwnersMarkedBrowserIsNotCounted(t *testing.T) {
	t.Parallel()
	e := start(t, func(c *config.Config) { c.DevLogin = true })
	view := func(b *browser) { b.hit("/api/a/view", map[string]string{"id": uuid.NewString(), "path": "/"}, nil) }

	visitor := e.browser()
	visitor.expect(401, "PUT", "/api/analytics/exclusion", map[string]bool{"excluded": true})
	visitor.expect(201, "POST", "/api/sandbox", nil)
	visitor.expect(403, "PUT", "/api/analytics/exclusion", map[string]bool{"excluded": true})
	visitor.expect(403, "GET", "/api/analytics/exclusion", nil)

	mine := e.browser()
	mine.expect(200, "POST", "/auth/dev", nil)
	if decode[map[string]bool](t, mine.expect(200, "GET", "/api/analytics/exclusion", nil))["excluded"] {
		t.Fatal("a browser starts unmarked")
	}
	mine.expect(200, "PUT", "/api/analytics/exclusion", map[string]bool{"excluded": true})
	if !decode[map[string]bool](t, mine.expect(200, "GET", "/api/analytics/exclusion", nil))["excluded"] {
		t.Fatal("the mark should be remembered")
	}
	mine.expect(204, "POST", "/auth/logout", nil)
	view(mine)
	if n := e.views(); n != 0 {
		t.Fatalf("a marked browser was counted after signing out: %d views", n)
	}

	// Unmarked again, it counts like anyone's.
	mine.expect(200, "POST", "/auth/dev", nil)
	mine.expect(200, "PUT", "/api/analytics/exclusion", map[string]bool{"excluded": false})
	mine.expect(204, "POST", "/auth/logout", nil)
	view(mine)
	if n := e.views(); n != 1 {
		t.Fatalf("an unmarked, signed-out browser should be counted: %d views", n)
	}
}

// Engaged readers are counted apart from everyone who merely loaded a page.
func TestEngagedReadersAreCountedSeparately(t *testing.T) {
	t.Parallel()
	e := start(t, func(c *config.Config) { c.DevLogin = true })
	ctx := context.Background()
	// Four visitors, told apart by their browsers.
	visit := func(agent, path string) string {
		id := uuid.NewString()
		e.browser().hit("/api/a/view", map[string]string{"id": id, "path": path}, map[string]string{"User-Agent": firefox + " " + agent})
		return id
	}
	event := func(agent, id, name string) {
		e.browser().hit("/api/a/event", map[string]string{"id": id, "name": name}, map[string]string{"User-Agent": firefox + " " + agent})
	}
	visit("glance", "/")
	reader := visit("reader", "/projects/redacted")
	if _, err := e.db.Owner.Exec(ctx, "UPDATE page_views SET engaged_seconds = 40 WHERE id = $1", reader); err != nil {
		t.Fatal(err)
	}
	event("opener", visit("opener", "/go/redacted"), "demo_open")
	event("waiter", visit("waiter", "/go/redacted"), "demo_ready") // happens by itself: not engagement

	rep := decode[struct {
		Summary  struct{ Views, Visitors, EngagedVisitors int }
		Projects []struct {
			Project                   string
			Visitors, EngagedVisitors int
		}
	}](t, func() string {
		owner := e.browser()
		owner.expect(200, "POST", "/auth/dev", nil)
		return owner.expect(200, "GET", "/api/analytics", nil)
	}())
	if rep.Summary.Views != 4 || rep.Summary.Visitors != 4 || rep.Summary.EngagedVisitors != 2 {
		t.Fatalf("summary: %+v, want 4 views, 4 visitors, 2 engaged", rep.Summary)
	}
	if len(rep.Projects) != 1 || rep.Projects[0].Visitors != 3 || rep.Projects[0].EngagedVisitors != 2 {
		t.Fatalf("projects: %+v, want redacted with 3 visitors, 2 engaged", rep.Projects)
	}
}

// Links pasted elsewhere get a preview card, and crawlers are told what to read.
func TestLinkPreviewsAndCrawlers(t *testing.T) {
	t.Parallel()
	e := start(t, nil, testSite(t, "http://127.0.0.1:1"))
	b := e.browser()
	for path, title := range map[string]string{
		"/":                  `<meta property="og:title" content="The Lighthouse · Martin">`,
		"/projects":          `<meta property="og:title" content="Projects · Martin">`,
		"/projects/redacted": `<meta property="og:title" content="Redacted: Words black out">`,
		"/about":             `<meta property="og:title" content="About Martin">`,
		"/status":            `<meta property="og:title" content="Systems data · Martin">`,
	} {
		body := b.expect(200, "GET", path, nil)
		for _, want := range []string{title, `<link rel="canonical" href="` + e.url + path + `">`, `<meta property="og:description" content="`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: missing %s", path, want)
			}
		}
	}

	robots := b.expect(200, "GET", "/robots.txt", nil)
	for _, want := range []string{"Disallow: /console/", "Disallow: /api/", "Disallow: /go/", "Sitemap: " + e.url + "/sitemap.xml"} {
		if !strings.Contains(robots, want) {
			t.Errorf("robots.txt: missing %q", want)
		}
	}
	sitemap := b.expect(200, "GET", "/sitemap.xml", nil)
	for _, path := range []string{"/", "/projects", "/projects/redacted", "/about", "/status", "/privacy"} {
		if !strings.Contains(sitemap, "<loc>"+e.url+path+"</loc>") {
			t.Errorf("sitemap: missing %s", path)
		}
	}
	if strings.Contains(sitemap, "/projects/tool") || strings.Contains(sitemap, "/go/") {
		t.Error("sitemap lists a page that doesn't exist or shouldn't be crawled")
	}
}
