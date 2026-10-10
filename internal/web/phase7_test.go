package web_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/MrtnOmwenga/lighthouse/internal/config"
	"github.com/MrtnOmwenga/lighthouse/internal/web"
)

// A tagged link's report shows what its visitor did, counts nothing twice, and the owner is told
// the first time each day that the link is opened.
func TestTaggedVisitsAreReportedAndAnnounced(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var told []string
	e := startWith(t, func(c *config.Config) { c.DevLogin = true }, func(s *web.Server) {
		s.Analytics.TagOpened = func(_ context.Context, tag, page, device string) {
			mu.Lock()
			told = append(told, tag+" "+page+" "+device)
			mu.Unlock()
		}
	})
	b := e.browser()
	hit := func(path string, body map[string]string) { b.hit(path, body, nil) }
	first, second := uuid.NewString(), uuid.NewString()
	hit("/api/a/view", map[string]string{"id": first, "path": "/", "ref": "acme-12"})
	for _, name := range []string{"cv_download", "outbound_github", "contact_email", "cv_download", "not_an_event"} {
		hit("/api/a/event", map[string]string{"id": first, "name": name})
	}
	hit("/api/a/view", map[string]string{"id": second, "path": "/projects/redacted"})
	hit("/api/a/event", map[string]string{"id": second, "name": "read_to_end"})
	// The same link opened again that day: counted, not announced again.
	hit("/api/a/view", map[string]string{"id": uuid.NewString(), "path": "/", "ref": "acme-12"})

	mu.Lock()
	if len(told) != 1 || told[0] != "acme-12 / desktop" {
		t.Fatalf("the owner should be told once: %v", told)
	}
	mu.Unlock()

	owner := e.browser()
	owner.expect(200, "POST", "/auth/dev", nil)
	rep := decode[struct {
		Refs []struct {
			Ref     string   `json:"ref"`
			Views   int      `json:"views"`
			Actions []string `json:"actions"`
		} `json:"refs"`
		Actions []struct {
			Label    string `json:"label"`
			Visitors int    `json:"visitors"`
		} `json:"actions"`
	}](t, owner.expect(200, "GET", "/api/analytics", nil))
	if len(rep.Refs) != 1 || rep.Refs[0].Views != 3 {
		t.Fatalf("three views, however many things were done on them: %+v", rep.Refs)
	}
	if got := strings.Join(rep.Refs[0].Actions, ","); got != "contact_email,cv_download,outbound_github,read_to_end" {
		t.Fatalf("what the tagged visitor did: %s", got)
	}
	if len(rep.Actions) != 4 {
		t.Fatalf("site-wide actions: %+v", rep.Actions)
	}
	for _, a := range rep.Actions {
		if a.Visitors != 1 {
			t.Fatalf("one visitor did each: %+v", rep.Actions)
		}
	}
}

// The assistant is a separate service; this site carries only its window and its privacy notice.
// The notice has to be there before anything is stored, so it ships with the window.
func TestTheGuideShipsWithItsPrivacyNotice(t *testing.T) {
	t.Parallel()
	e := start(t, nil)
	b := e.browser()
	privacy := b.expect(200, "GET", "/privacy", nil)
	for _, want := range []string{`id="assistant"`, "stored for 90 days", "Anthropic", "Amazon Web Services", "Email addresses and phone numbers are taken out"} {
		if !strings.Contains(privacy, want) {
			t.Errorf("the privacy page doesn't say %q", want)
		}
	}
	front := b.expect(200, "GET", "/", nil)
	if !strings.Contains(front, `/static/guide.js?v=`) {
		t.Fatal("the guide's script isn't on the page")
	}
	// Its styles are loaded by the script, only once the assistant is on.
	if !strings.Contains(front, `data-css="/static/guide.css?v=`) || strings.Contains(b.expect(200, "GET", "/static/style.css", nil), ".guide-panel") {
		t.Error("the guide's styles should be a file of their own, named to the script")
	}
	if css := b.expect(200, "GET", "/static/guide.css", nil); !strings.Contains(css, ".guide-panel") || !strings.Contains(css, "--claret") {
		t.Error("guide.css should carry the window's styles and the colours it needs away from the site")
	}
	if !strings.Contains(privacy, "it comes with you") || !strings.Contains(privacy, "not added to GhostChat") {
		t.Error("the privacy page doesn't say the assistant follows into Redacted and stays out of GhostChat")
	}
	script := b.expect(200, "GET", "/static/guide.js", nil)
	// It is inert unless switched on, sends no cookies, and never writes a reply as markup.
	for _, want := range []string{"localStorage.getItem('lh_guide') === '1'", "credentials: 'omit'", "textContent", "window.top !== window"} {
		if !strings.Contains(script, want) {
			t.Errorf("guide.js: missing %q", want)
		}
	}
	if strings.Contains(script, "innerHTML") {
		t.Error("guide.js writes markup")
	}
}
