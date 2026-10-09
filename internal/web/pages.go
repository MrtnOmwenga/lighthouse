package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MrtnOmwenga/lighthouse/internal/site"
	"github.com/MrtnOmwenga/lighthouse/internal/status"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
)

var funcs = template.FuncMap{
	"sparkline": func(m status.Monitor, now time.Time) template.HTML {
		return status.Sparkline(m.Latency, 24*time.Hour, now)
	},
	"pct":   pct,
	"asset": asset,
	"ms": func(v *float64) string {
		if v == nil {
			return "–"
		}
		return fmt.Sprintf("%.0f ms", *v)
	},
	"when": func(t time.Time) string { return t.UTC().Format("2 Jan 2006, 15:04 UTC") },
	"ago":  ago,
	"duration": func(i store.Incident, now time.Time) string {
		end := now
		if i.ResolvedAt != nil {
			end = *i.ResolvedAt
		}
		return humanDuration(end.Sub(i.StartedAt))
	},
	"inc":    func(i int) int { return i + 1 },
	"anchor": site.Anchor,
	// stage gathers what the "arch-stage" template needs. Embedded in another page, its parts link
	// to the architecture page; on that page they link to the list below the diagram.
	"stage": func(c *card, a *site.Architecture, embedded bool) map[string]any {
		m := map[string]any{"Arch": a, "Slug": c.Slug, "Embedded": embedded, "Base": "", "HasStory": c.HasStory}
		if embedded {
			m["Base"] = "/projects/" + c.Slug + "/architecture"
		}
		return m
	},
	// lowerFirst lets a title continue a sentence: "A Director demotes" becomes "a Director demotes".
	"lowerFirst": func(v string) string {
		if v == "" {
			return v
		}
		return strings.ToLower(v[:1]) + v[1:]
	},
	"minutes": func(m float64) string {
		if m < 1 {
			return "under a minute"
		}
		return fmt.Sprintf("%.0f min", m)
	},
	// figure selects an illustration and its size for the "figure" template.
	"figure": func(kind, variant string) map[string]string {
		return map[string]string{"Kind": kind, "Variant": variant}
	},
	"dateline": func(t time.Time) string { return t.In(nairobi).Format("Monday 2 January 2006") },
	"words":    func(s string) string { return strings.ReplaceAll(s, "_", " ") },
	// social gathers what the "social" template needs: the page's address, and the title and
	// description a link preview shows.
	"social": func(d pageData, path, title, description string) map[string]string {
		m := map[string]string{"URL": d.Base + path, "Title": title, "Description": description}
		if d.Site != nil && d.Site.Profile.Preview != "" {
			m["Image"] = d.Base + "/media/" + d.Site.Profile.Preview
		}
		return m
	},
	// latest is the most recent update written for people (not a status or severity change).
	"latest": func(events []store.Event) *store.Event {
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Kind == "comment" || events[i].Kind == "opened" {
				return &events[i]
			}
		}
		return nil
	},
}

// nairobi is the dateline's time zone (fixed: Kenya has no daylight saving).
var nairobi = time.FixedZone("EAT", 3*60*60)

func pct(p *float64) string {
	if p == nil {
		return "–"
	}
	if *p == 100 {
		return "100%"
	}
	return fmt.Sprintf("%.2f%%", *p)
}

func msOrEmpty(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.0f ms", *v)
}

func ago(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	return humanDuration(d) + " ago"
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		h, m := int(d.Hours()), int(d.Minutes())%60
		if m == 0 {
			return plural(h, "hour")
		}
		return plural(h, "hour") + " " + plural(m, "minute")
	default:
		return plural(int(d.Hours()/24), "day")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

type pageData struct {
	Owner   string
	Base    string // the site's public address, for absolute links
	Section string // highlighted in the navigation
	Now     time.Time
	Site    *site.Site
	Status  status.Page
	Stale   bool // Status couldn't be rebuilt just now; it is the last one that was
	// front page
	Ticker                         []tickerItem
	Leads, Sides, Features, Briefs []card
	Incidents14d                   int
	// projects index
	Cards     []card
	LiveCount int
	// a project's story and launch page
	Card  *card
	Story *site.Story
	// a project's architecture page; Draft keeps it out of search engines
	Architecture *site.Architecture
	Draft        bool
	Next         *card
	// systems page
	Readership []readershipRow
	// incident page
	Incident *status.Incident
	// error page
	Title, Message string
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data pageData) {
	data.Owner = s.Config.OwnerName
	data.Base = s.Config.PublicURL
	data.Site = s.Site
	if data.Now.IsZero() {
		data.Now = s.Now()
	}
	var buf bytes.Buffer
	if err := s.pages.ExecuteTemplate(&buf, name, data); err != nil {
		s.Log.Error("rendering", "template", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

func (s *Server) renderError(w http.ResponseWriter, code int, title, message string) {
	s.render(w, code, "error.html", pageData{Title: title, Message: message})
}

func (s *Server) statusPage(w http.ResponseWriter, r *http.Request) {
	now := s.Now()
	page, stale, err := s.ownerStatus(r.Context())
	if err != nil {
		s.Log.Error("status page", "err", err)
		s.renderError(w, http.StatusServiceUnavailable, "Status unavailable", "The status page couldn't be built just now. Please try again in a minute.")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=15")
	s.render(w, http.StatusOK, "status.html", pageData{Section: "status", Now: now, Status: page, Stale: stale, Incidents14d: len(page.Active) + len(page.Recent), Readership: s.readership(r)})
}

func (s *Server) publicStatus(w http.ResponseWriter, r *http.Request) {
	s.errs(func(w http.ResponseWriter, r *http.Request) error {
		page, _, err := s.ownerStatus(r.Context())
		if err != nil {
			return err
		}
		w.Header().Set("Cache-Control", "public, max-age=15")
		w.Header().Set("Access-Control-Allow-Origin", "*") // public data, readable by the hub and badges
		return writeJSON(w, http.StatusOK, page)
	})(w, r)
}

// incidentPage shows one public incident and its public timeline. Private incidents are
// indistinguishable from missing ones.
func (s *Server) incidentPage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		s.renderError(w, http.StatusNotFound, "Not found", "There's no public incident here.")
		return
	}
	var view status.Incident
	err = store.WithTenant(r.Context(), s.Pool, s.OwnerTenant, func(tx pgx.Tx) error {
		inc, err := store.GetIncident(r.Context(), tx, id)
		if err != nil {
			return err
		}
		if !inc.Public {
			return store.ErrNotFound
		}
		events, err := store.Events(r.Context(), tx, id, true)
		view = status.Incident{Incident: inc, Events: events}
		return err
	})
	if err != nil {
		if err != store.ErrNotFound {
			s.Log.Error("incident page", "err", err)
		}
		s.renderError(w, http.StatusNotFound, "Not found", "There's no public incident here.")
		return
	}
	s.render(w, http.StatusOK, "incident.html", pageData{Section: "status", Incident: &view})
}
