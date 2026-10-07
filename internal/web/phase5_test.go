package web_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/MrtnOmwenga/lighthouse/internal/config"
)

// Over HTTPS the session cookie is bound to this exact host: a cookie of the same name set for
// the parent domain (by a page on a sibling address) is not read.
func TestSessionCookieIsHostBound(t *testing.T) {
	t.Parallel()
	const public = "https://lighthouse.example"
	e := start(t, func(c *config.Config) { c.DevLogin = true; c.PublicURL = public })
	b := e.browser()
	resp, _ := b.do("POST", "/auth/dev", nil, public)
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "__Host-lh_session" {
			session = c
		}
	}
	if session == nil || !session.Secure || !session.HttpOnly || session.Path != "/" || session.Domain != "" {
		t.Fatalf("session cookie: %+v (all: %v)", session, resp.Cookies())
	}
	me := func(cookie string) int {
		req, _ := http.NewRequest("GET", e.url+"/api/me", nil)
		req.Header.Set("Cookie", cookie)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		r.Body.Close()
		return r.StatusCode
	}
	if code := me("__Host-lh_session=" + session.Value); code != 200 {
		t.Fatalf("the host-bound cookie should sign in: %d", code)
	}
	if code := me("lh_session=" + session.Value); code != 401 {
		t.Fatalf("a cookie without the prefix must be ignored, even with a valid token: %d", code)
	}
}

// Sign-ins are recorded, refused ones included, and only the owner can read the record.
func TestSignInsAreRecorded(t *testing.T) {
	t.Parallel()
	e := start(t, nil)
	signIn := func(b *browser, code string) int {
		resp, _ := b.do("GET", "/auth/github", nil)
		loc, _ := url.Parse(resp.Header.Get("Location"))
		resp, _ = b.do("GET", "/auth/github/callback?"+url.Values{"code": {code}, "state": {loc.Query().Get("state")}}.Encode(), nil)
		return resp.StatusCode
	}
	if code := signIn(e.browser(), "stranger"); code != 403 {
		t.Fatalf("stranger: %d", code)
	}
	owner := e.browser()
	if code := signIn(owner, "owner"); code != 302 {
		t.Fatalf("owner: %d", code)
	}

	type security struct {
		Sessions []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
			Device  string `json:"device"`
		} `json:"sessions"`
		Events []struct {
			Kind     string `json:"kind"`
			Login    string `json:"login"`
			GitHubID *int64 `json:"githubId"`
		} `json:"events"`
	}
	raw := owner.expect(200, "GET", "/api/security", nil)
	got := decode[security](t, raw)
	if len(got.Events) != 2 || got.Events[0].Kind != "signed_in" || got.Events[0].Login != "MrtnOmwenga" ||
		got.Events[1].Kind != "refused" || got.Events[1].Login != "stranger" || got.Events[1].GitHubID == nil || *got.Events[1].GitHubID != 7 {
		t.Fatalf("events, newest first: %+v", got.Events)
	}
	if len(got.Sessions) != 1 || !got.Sessions[0].Current || got.Sessions[0].Device != "desktop" {
		t.Fatalf("sessions: %+v", got.Sessions)
	}
	if strings.Contains(raw, "token") || strings.Contains(raw, "hash") {
		t.Fatalf("the answer must not carry tokens or their hashes: %s", raw)
	}

	visitor := e.browser()
	visitor.expect(401, "GET", "/api/security", nil)
	visitor.expect(201, "POST", "/api/sandbox", nil)
	visitor.expect(403, "GET", "/api/security", nil)
	visitor.expect(403, "POST", "/api/sessions/end-others", nil)
}

// The owner can end one session, or every session but the one in hand.
func TestSessionsCanBeEnded(t *testing.T) {
	t.Parallel()
	e := start(t, func(c *config.Config) { c.DevLogin = true })
	here, laptop, phone := e.browser(), e.browser(), e.browser()
	for _, b := range []*browser{here, laptop, phone} {
		b.expect(200, "POST", "/auth/dev", nil)
	}
	type session struct {
		ID      string `json:"id"`
		Current bool   `json:"current"`
	}
	list := func() []session {
		return decode[struct {
			Sessions []session `json:"sessions"`
		}](t, here.expect(200, "GET", "/api/security", nil)).Sessions
	}
	sessions := list()
	if len(sessions) != 3 {
		t.Fatalf("%d sessions, want 3", len(sessions))
	}
	var other string
	for _, s := range sessions {
		if !s.Current {
			other = s.ID
		}
	}

	here.expect(204, "DELETE", "/api/sessions/"+other, nil)
	here.expect(404, "DELETE", "/api/sessions/"+other, nil)
	if n := len(list()); n != 2 {
		t.Fatalf("%d sessions after ending one, want 2", n)
	}
	if got := decode[map[string]int](t, here.expect(200, "POST", "/api/sessions/end-others", nil)); got["ended"] != 1 {
		t.Fatalf("end-others: %v", got)
	}
	here.expect(200, "GET", "/api/me", nil)
	laptop.expect(401, "GET", "/api/me", nil)
	phone.expect(401, "GET", "/api/me", nil)

	// A sandbox can't end the owner's sessions, by id or otherwise.
	visitor := e.browser()
	visitor.expect(201, "POST", "/api/sandbox", nil)
	visitor.expect(403, "DELETE", "/api/sessions/"+list()[0].ID, nil)
}
