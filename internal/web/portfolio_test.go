package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MrtnOmwenga/lighthouse/internal/site"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
)

// testSite writes a small site folder: a demo with a story and a health address, a project
// without a demo, a profile with a portrait.
func testSite(t *testing.T, health string) *site.Site {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"media/portrait.jpg": "\xff\xd8\xff\xe0 not really a jpeg",
		"site.yaml": `
profile:
  strip: A live portfolio.
  portrait: portrait.jpg
  links: { email: me@example.com, github: "https://github.com/example" }
  lead: { kicker: About the engineer, headline: Builds backends, standfirst: Nairobi. }
  glance: [{ label: Role, value: Tech lead }]
  how_to: [{ title: Launch a demo, body: It wakes up. }]
  about:
    headline: A profile headline
    paragraphs: [Career so far.]
    career: [{ when: "2024 – now", title: Tech lead, body: Leads things., note: Go }]
    contact: Nairobi hours.
projects:
  - slug: redacted
    name: Redacted
    kicker: Security
    headline: Words black out
    plain: Classified words.
    hood: Row-level security.
    figure: redacted
    placement: lead
    repo: https://github.com/example/redacted
    demo: https://redacted.example
    health: ` + health + `/internal-health
    monitor: redacted
    intro:
      - { kicker: Architecture, title: A briefing room, plain: Three screens., body: One document per section. }
      - { kicker: Proof, title: Tests, body: Six hundred of them. }
  - slug: tool
    name: Tool
    headline: Runs locally
    plain: A local tool.
    hood: FastAPI.
    placement: brief
    repo: https://github.com/example/tool
    no_demo_note: Install it yourself.
`,
		"stories/redacted.yaml": `
problem: { title: The problem, paragraphs: [Documents leak hidden text.] }
solution: { title: How it works, paragraphs: [Hiding by structure.] }
decisions:
  - { title: Hide by structure, choice: One document per section., why: CRDTs can't be filtered., trade_off: Moving text is a copy. }
testing: { title: How it is tested, paragraphs: [603 tests.] }
limits: { title: What it doesn't do yet, paragraphs: [Multi-instance locking.] }
numbers: [{ value: "603", label: tests }]
`,
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := site.Load(dir, "example.dev")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var inlineStyle = regexp.MustCompile(`\sstyle="`)

func TestPortfolioPages(t *testing.T) {
	t.Parallel()
	var up atomic.Bool
	demo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer demo.Close()
	e := start(t, nil, testSite(t, demo.URL))
	e.ownerData(func(tx pgx.Tx) error {
		m, err := store.CreateMonitor(context.Background(), tx, e.owner, store.MonitorInput{Name: "Redacted", Slug: "redacted", Kind: "simulated",
			SimulatedMode: "up", IntervalSeconds: 60, TimeoutMS: 1000, ExpectedStatusMin: 200, ExpectedStatusMax: 299,
			FailureThreshold: 1, RecoveryThreshold: 1, Public: true})
		if err != nil {
			return err
		}
		m.Health = "up"
		return store.SaveMonitorState(context.Background(), tx, m)
	})
	b := e.browser()

	pages := map[string][]string{
		"/":                  {"A live portfolio.", "Builds backends", "Words black out", `href="/go/redacted"`, `href="/projects/redacted"`, "LIVE SYSTEMS", "Systems data", "/media/portrait.jpg"},
		"/projects":          {"2 projects, 1 running live", "Words black out", "Install it yourself."},
		"/projects/redacted": {"The problem", "Hide by structure", "The trade-off.", "What it doesn&#39;t do yet", "603"},
		"/about":             {"A profile headline", "Tech lead", "Nairobi hours.", "mailto:me@example.com"},
		"/go/redacted":       {"Starting Redacted", "Part 1 of 2 · Architecture", "One document per section.", `data-demo="https://redacted.example"`, "/static/launch.js"},
		"/status":            {"Systems data", "Redacted"},
	}
	for path, wants := range pages {
		resp, body := b.do("GET", path, nil)
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d", path, resp.StatusCode)
			continue
		}
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %q", path, want)
			}
		}
		// The CSP blocks inline styles, so a page that uses them would render unstyled.
		if inlineStyle.MatchString(body) {
			t.Errorf("%s uses an inline style attribute", path)
		}
		// The health address is internal and never appears in public output.
		if strings.Contains(body, "internal-health") {
			t.Errorf("%s reveals the health address", path)
		}
	}

	for _, path := range []string{"/projects/tool", "/projects/nope", "/go/tool", "/go/nope", "/media/nope.jpg", "/media/..%2fsite.yaml", "/media/site.yaml"} {
		if resp, _ := b.do("GET", path, nil); resp.StatusCode != 404 {
			t.Errorf("%s: %d, want 404", path, resp.StatusCode)
		}
	}
	if resp, body := b.do("GET", "/media/portrait.jpg", nil); resp.StatusCode != 200 || !strings.HasPrefix(body, "\xff\xd8") {
		t.Errorf("the portrait: %d", resp.StatusCode)
	}

	_, api := b.do("GET", "/api/projects", nil)
	if strings.Contains(api, "internal-health") || !strings.Contains(api, `"launch":"/go/redacted"`) || !strings.Contains(api, `"story":"/projects/redacted"`) {
		t.Errorf("api: %s", api)
	}
	if body := b.expect(200, "GET", "/api/projects/redacted/ready", nil); !strings.Contains(body, `"ready":false`) {
		t.Fatalf("a demo answering 503 isn't ready: %s", body)
	}
	up.Store(true)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(b.expect(200, "GET", "/api/projects/redacted/ready", nil), `"ready":true`) {
		if time.Now().After(deadline) {
			t.Fatal("never became ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
	b.expect(404, "GET", "/api/projects/tool/ready", nil)
}

// The content that ships renders on every page, with no template errors and no inline styles.
func TestTheShippedSiteRenders(t *testing.T) {
	t.Parallel()
	content, err := site.Load("../../deploy/site", "example.dev")
	if err != nil {
		t.Fatal(err)
	}
	e := start(t, nil, content)
	b := e.browser()
	paths := []string{"/", "/projects", "/about", "/status"}
	for _, p := range content.Projects {
		if content.Stories[p.Slug] != nil {
			paths = append(paths, "/projects/"+p.Slug)
		}
		if p.Demo != "" {
			paths = append(paths, "/go/"+p.Slug)
		}
	}
	for _, path := range paths {
		resp, body := b.do("GET", path, nil)
		if resp.StatusCode != 200 || !strings.Contains(body, "</html>") {
			t.Errorf("%s: %d", path, resp.StatusCode)
		}
		if inlineStyle.MatchString(body) {
			t.Errorf("%s uses an inline style attribute", path)
		}
		if strings.Contains(body, "svc.cluster.local") {
			t.Errorf("%s reveals an internal address", path)
		}
	}
}

// A project's architecture page: the diagram's parts and walk-through are in the page itself, so
// it reads without its script. A draft answers but isn't linked, listed or indexed.
func TestTheArchitecturePage(t *testing.T) {
	t.Parallel()
	content, err := site.Load("../../deploy/site", "example.dev")
	if err != nil {
		t.Fatal(err)
	}
	for i := range content.Projects {
		content.Projects[i].ArchitectureDraft = content.Projects[i].Slug != "redacted"
	}
	e := start(t, nil, content)
	b := e.browser()
	resp, page := b.do("GET", "/projects/redacted/architecture", nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-Robots-Tag") != "" {
		t.Fatalf("a published diagram: %d, robots %q", resp.StatusCode, resp.Header.Get("X-Robots-Tag"))
	}
	for _, want := range []string{
		`id="part-realtime"`, `href="#about-realtime"`, `id="about-realtime"`, `id="flow-demote-3"`, `data-from="api" data-to="realtime"`,
		`href="https://github.com/MrtnOmwenga/RBAC-API/blob/HEAD/src/policy/policy.ts"`, `/static/arch.js?v=`, "REST API → Collaboration server",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page is missing %q", want)
		}
	}
	if inlineStyle.MatchString(page) || strings.Contains(page, "noindex") {
		t.Error("the page has an inline style, or hides from search engines though it is published")
	}
	if !strings.Contains(b.expect(200, "GET", "/projects/redacted", nil), `href="/projects/redacted/architecture"`) {
		t.Error("the story doesn't link to its diagram")
	}
	if !strings.Contains(b.expect(200, "GET", "/sitemap.xml", nil), "/projects/redacted/architecture") {
		t.Error("the sitemap leaves the diagram out")
	}
	b.expect(404, "GET", "/projects/ghostchat/architecture", nil)
	b.expect(404, "GET", "/projects/nothing/architecture", nil)

	for i := range content.Projects {
		content.Projects[i].ArchitectureDraft = true
	}
	resp, page = b.do("GET", "/projects/redacted/architecture", nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-Robots-Tag") != "noindex" || !strings.Contains(page, `<meta name="robots" content="noindex">`) {
		t.Errorf("a draft answers, and asks not to be indexed: %d %q", resp.StatusCode, resp.Header.Get("X-Robots-Tag"))
	}
	if strings.Contains(b.expect(200, "GET", "/projects/redacted", nil), "/architecture") || strings.Contains(b.expect(200, "GET", "/sitemap.xml", nil), "/architecture") {
		t.Error("a draft is linked from the story or listed in the sitemap")
	}
}
