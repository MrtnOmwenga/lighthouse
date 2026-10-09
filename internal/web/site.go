package web

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/MrtnOmwenga/lighthouse/internal/site"
	"github.com/MrtnOmwenga/lighthouse/internal/status"
)

// card is a project as the pages show it: its content plus its live status, when it has a public
// monitor.
type card struct {
	site.Project
	Health    string   // up, down or unknown; empty without a public monitor
	Uptime90d *float64 // percent
	HasStory  bool
	// HasDiagram: the project has an architecture page that is ready to be linked to.
	HasDiagram bool
}

type tickerItem struct {
	Name, Value, Detail string
	Down                bool
}

func (s *Server) cards(page status.Page) []card {
	monitors := map[string]status.Monitor{}
	for _, m := range page.Monitors {
		monitors[m.Slug] = m
	}
	out := make([]card, len(s.Site.Projects))
	for i, p := range s.Site.Projects {
		c := card{Project: p, HasStory: s.Site.Stories[p.Slug] != nil}
		c.HasDiagram = s.Site.Architectures[p.Slug] != nil && !p.ArchitectureDraft
		if m, ok := monitors[p.Monitor]; ok && p.Monitor != "" {
			c.Health, c.Uptime90d = m.Health, m.Uptime90d
		}
		out[i] = c
	}
	return out
}

func byPlacement(cards []card, p site.Placement) []card {
	var out []card
	for _, c := range cards {
		if c.Placement == p {
			out = append(out, c)
		}
	}
	return out
}

// statusOrEmpty builds the public status; the portfolio pages still render without it.
func (s *Server) statusOrEmpty(r *http.Request) status.Page {
	page, _, err := s.ownerStatus(r.Context())
	if err != nil {
		s.Log.Warn("status unavailable for a portfolio page", "err", err)
	}
	return page
}

func (s *Server) frontPage(w http.ResponseWriter, r *http.Request) {
	page := s.statusOrEmpty(r)
	cards := s.cards(page)
	var ticker []tickerItem
	checks := 0
	for _, m := range page.Monitors {
		value := "checking"
		switch m.Health {
		case "up":
			value = "up · " + pct(m.Uptime90d)
		case "down":
			value = "down"
		}
		ticker = append(ticker, tickerItem{Name: m.Name, Value: value, Detail: msOrEmpty(m.P50), Down: m.Health == "down"})
		checks += m.Checks24h
	}
	if len(page.Monitors) > 0 {
		incidents := len(page.Active) + len(page.Recent)
		ticker = append(ticker,
			tickerItem{Name: "Checks, 24 h", Value: fmt.Sprintf("%d", checks)},
			tickerItem{Name: "Incidents, 14 days", Value: fmt.Sprintf("%d", incidents), Detail: fmt.Sprintf("%d open", len(page.Active))},
		)
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	s.render(w, http.StatusOK, "front.html", pageData{
		Section: "front", Status: page, Ticker: ticker,
		Leads: byPlacement(cards, site.Lead), Sides: byPlacement(cards, site.Side),
		Features: byPlacement(cards, site.Feature), Briefs: byPlacement(cards, site.Brief),
	})
}

func (s *Server) projectsPage(w http.ResponseWriter, r *http.Request) {
	cards := s.cards(s.statusOrEmpty(r))
	live := 0
	for _, c := range cards {
		if c.Demo != "" {
			live++
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	s.render(w, http.StatusOK, "projects.html", pageData{Section: "projects", Cards: cards, LiveCount: live})
}

func (s *Server) storyPage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	story := s.Site.Stories[slug]
	if story == nil {
		s.renderError(w, http.StatusNotFound, "No story here", "There's no project by that name. The projects page lists them all.")
		return
	}
	cards := s.cards(s.statusOrEmpty(r))
	data := pageData{Section: "projects", Story: story}
	for i := range cards {
		if cards[i].Slug == slug {
			data.Card = &cards[i]
		}
	}
	if next, ok := s.Site.Next(slug); ok {
		for i := range cards {
			if cards[i].Slug == next.Slug {
				data.Next = &cards[i]
			}
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	s.render(w, http.StatusOK, "story.html", data)
}

// architecturePage draws a project's system: its parts, how they connect, and flows through them.
func (s *Server) architecturePage(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	arch := s.Site.Architectures[slug]
	if arch == nil {
		s.renderError(w, http.StatusNotFound, "No diagram here", "This project has no architecture page. The projects page lists them all.")
		return
	}
	project, _ := s.Site.Find(slug)
	data := pageData{Section: "projects", Architecture: arch, Draft: project.ArchitectureDraft}
	cards := s.cards(s.statusOrEmpty(r))
	for i := range cards {
		if cards[i].Slug == slug {
			data.Card = &cards[i]
		}
	}
	if data.Draft {
		w.Header().Set("X-Robots-Tag", "noindex")
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	s.render(w, http.StatusOK, "architecture.html", data)
}

func (s *Server) aboutPage(w http.ResponseWriter, _ *http.Request) {
	if s.Site.Profile.About.Headline == "" {
		s.renderError(w, http.StatusNotFound, "Not found", "There's no profile here yet.")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	s.render(w, http.StatusOK, "about.html", pageData{Section: "about"})
}

// media serves the site's own files (the portrait, the CV) by plain name: no paths, no listings.
func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.Site.Dir == "" || !site.MediaName(name) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if strings.HasSuffix(strings.ToLower(name), ".pdf") {
		w.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
	}
	http.ServeFile(w, r, filepath.Join(s.Site.Dir, "media", name))
}

// launchPage introduces a demo while it starts, then opens it (or offers a guided tour).
func (s *Server) launchPage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.Site.Find(r.PathValue("slug"))
	if !ok || p.Demo == "" {
		s.renderError(w, http.StatusNotFound, "Not found", "There's no demo by that name.")
		return
	}
	c := card{Project: p, HasStory: s.Site.Stories[p.Slug] != nil}
	s.render(w, http.StatusOK, "launch.html", pageData{Section: "projects", Card: &c})
}

// publicProject is what the API says about a project: never its health address, which usually
// points inside the cluster.
type publicProject struct {
	Slug     string       `json:"slug"`
	Name     string       `json:"name"`
	Headline string       `json:"headline"`
	Plain    string       `json:"plain"`
	Hood     string       `json:"hood"`
	Repo     string       `json:"repo,omitempty"`
	Demo     string       `json:"demo,omitempty"`
	Launch   string       `json:"launch,omitempty"` // the launch page, when there is a demo
	Story    string       `json:"story,omitempty"`
	Tour     string       `json:"tour,omitempty"`
	Intro    []site.Slide `json:"intro,omitempty"`
}

func (s *Server) listProjects(w http.ResponseWriter, _ *http.Request) {
	out := make([]publicProject, len(s.Site.Projects))
	for i, p := range s.Site.Projects {
		out[i] = publicProject{Slug: p.Slug, Name: p.Name, Headline: p.Headline, Plain: p.Plain, Hood: p.Hood,
			Repo: p.Repo, Demo: p.Demo, Tour: p.Tour, Intro: p.Intro}
		if p.Demo != "" {
			out[i].Launch = "/go/" + p.Slug
		}
		if s.Site.Stories[p.Slug] != nil {
			out[i].Story = "/projects/" + p.Slug
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	_ = writeJSON(w, http.StatusOK, out)
}

// projectReady reports whether a demo is up, waking it if it sleeps. Launch pages poll it.
func (s *Server) projectReady(w http.ResponseWriter, r *http.Request) {
	s.errs(func(w http.ResponseWriter, r *http.Request) error {
		p, ok := s.Site.Find(r.PathValue("slug"))
		if !ok || p.Demo == "" {
			return errNotFound
		}
		w.Header().Set("Cache-Control", "no-store")
		return writeJSON(w, http.StatusOK, map[string]bool{"ready": s.Readiness.Ready(r.Context(), p)})
	})(w, r)
}

// robots asks crawlers to index the pages people read, and to leave the console, the API and the
// launch pages (which wake demos) alone.
func (s *Server) robots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	fmt.Fprintf(w, "User-agent: *\nAllow: /\nDisallow: /console/\nDisallow: /api/\nDisallow: /auth/\nDisallow: /go/\n\nSitemap: %s/sitemap.xml\n", s.Config.PublicURL)
}

// sitemap lists the public pages: the fixed ones and a story per project that has one.
func (s *Server) sitemap(w http.ResponseWriter, _ *http.Request) {
	paths := []string{"/", "/projects"}
	for _, p := range s.Site.Projects {
		if s.Site.Stories[p.Slug] != nil {
			paths = append(paths, "/projects/"+p.Slug)
		}
		if s.Site.Architectures[p.Slug] != nil && !p.ArchitectureDraft {
			paths = append(paths, "/projects/"+p.Slug+"/architecture")
		}
	}
	if s.Site.Profile.About.Headline != "" {
		paths = append(paths, "/about")
	}
	paths = append(paths, "/status", "/privacy")
	var b strings.Builder
	b.WriteString(xml.Header + `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` + "\n")
	for _, p := range paths {
		b.WriteString("  <url><loc>")
		_ = xml.EscapeText(&b, []byte(s.Config.PublicURL+p))
		b.WriteString("</loc></url>\n")
	}
	b.WriteString("</urlset>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(b.String()))
}
