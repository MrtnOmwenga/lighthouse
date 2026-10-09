package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MrtnOmwenga/lighthouse/internal/monitor"
)

func TestTheShippedSiteIsValid(t *testing.T) {
	s, err := Load("../../deploy/site", "example.dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Projects) != 4 {
		t.Fatalf("%d projects", len(s.Projects))
	}
	for _, p := range s.Projects {
		st := s.Stories[p.Slug]
		if st == nil {
			t.Errorf("%s has no story", p.Slug)
			continue
		}
		if len(st.Decisions) < 3 || st.Testing.Title == "" || st.Limits.Title == "" {
			t.Errorf("%s: a story needs its decisions, testing and limits", p.Slug)
		}
	}
	for _, placement := range []Placement{Lead, Side, Feature, Brief} {
		if len(s.ByPlacement(placement)) == 0 {
			t.Errorf("nothing placed at %s", placement)
		}
	}
	if next, ok := s.Next("pair-bridge"); !ok || next.Slug != "redacted" {
		t.Errorf("the last story's next should wrap to the first: %v", next.Slug)
	}
	if r, _ := s.Find("redacted"); r.Demo != "https://redacted.example.dev" || r.Tour != "https://redacted.example.dev/?tour=play" {
		t.Errorf("${DOMAIN} is the public URL's host: %q %q", r.Demo, r.Tour)
	}
	// The CV is linked from where it is kept, outside the repository (deploy/README.md).
	if cv := s.Profile.Links.CVURL(); !strings.HasPrefix(cv, "https://") || !strings.HasSuffix(cv, ".pdf") {
		t.Errorf("the CV link should be an https address of a PDF: %q", cv)
	}
}

func TestAnEmptyDirIsAnEmptySite(t *testing.T) {
	s, err := Load("", "example.dev")
	if err != nil || len(s.Projects) != 0 || s.Stories == nil {
		t.Fatalf("%+v %v", s, err)
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidationListsEveryProblem(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "site.yaml", `
profile:
  portrait: missing.jpg
  links: { email: not-an-email, github: "javascript:alert(1)", cv: ../../etc/passwd }
projects:
  - slug: Bad Slug
    name: ""
    headline: h
    plain: p
    hood: h
    figure: hologram
    demo: "//evil.example"
  - slug: dup
    name: A
    headline: h
    plain: p
    hood: h
    health: http://internal/health
  - slug: dup
    name: B
    headline: h
    plain: p
    hood: h
    demo: https://b.example
  - slug: storied
    name: C
    headline: h
    plain: p
    hood: h
`)
	write(t, dir, "stories/storied.yaml", "problem: { title: The problem }\n")
	_, err := Load(dir, "example.dev")
	if err == nil {
		t.Fatal("accepted invalid content")
	}
	for _, want := range []string{
		"profile.portrait: media/missing.jpg is missing", "profile.links.email", "profile.links.github",
		"profile.links.cv: a file name in media/", "slug must be", "name is required", "figure must be",
		"project Bad Slug: demo", "duplicate slug", "health, tour and intro need a demo",
		"a demo needs an intro", "story storied: needs a problem, a solution and at least one decision",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestUnknownKeysAreErrors(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "site.yaml", "projects:\n  - slug: x\n    nmae: typo\n")
	if _, err := Load(dir, "example.dev"); err == nil || !strings.Contains(err.Error(), "nmae") {
		t.Fatalf("a typo must fail loudly: %v", err)
	}
}

func TestMediaNames(t *testing.T) {
	for name, ok := range map[string]bool{
		"portrait.jpg": true, "cv-2026.pdf": true, "../secret": false, "a/b.jpg": false, ".env": false, "": false, "x..y": false,
	} {
		if MediaName(name) != ok {
			t.Errorf("MediaName(%q) = %v", name, !ok)
		}
	}
}

func TestReadinessSharesProbesAndCaches(t *testing.T) {
	var hits atomic.Int32
	var up atomic.Bool
	demo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(50 * time.Millisecond)
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer demo.Close()
	r := NewReadiness(monitor.NewProber())
	r.TTL = 200 * time.Millisecond
	p := Project{Slug: "demo", Health: demo.URL}

	var wg sync.WaitGroup
	for range 25 {
		wg.Go(func() {
			if r.Ready(context.Background(), p) {
				t.Error("a 503 isn't ready")
			}
		})
	}
	wg.Wait()
	if n := hits.Load(); n != 1 {
		t.Fatalf("25 simultaneous visitors caused %d probes, want 1", n)
	}
	up.Store(true)
	if r.Ready(context.Background(), p) {
		t.Fatal("the cached answer holds until it expires")
	}
	time.Sleep(250 * time.Millisecond)
	if !r.Ready(context.Background(), p) || hits.Load() != 2 {
		t.Fatalf("after the TTL it asks again: hits %d", hits.Load())
	}
	if !r.Ready(context.Background(), Project{Slug: "static"}) {
		t.Fatal("a demo without a health address is always ready")
	}
}
