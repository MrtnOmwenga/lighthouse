// Package analytics counts visits without cookies, local storage or stored IP addresses.
//
// A visitor is an HMAC-SHA256 of their IP address and user agent, keyed with a salt that lives for
// one UTC day: enough to count unique visitors and follow one visit across pages, not enough to
// recognise anyone the next day or recover an IP. Browsers that send Global Privacy Control or Do
// Not Track are not counted, nor are bots or the signed-in owner.
package analytics

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrtnOmwenga/lighthouse/internal/store"
)

type Recorder struct {
	Pool        *pgxpool.Pool
	OwnerTenant string
	Host        string // the site's own host, left out of referrers
	Now         func() time.Time

	mu   sync.Mutex
	day  string
	salt []byte
}

func New(pool *pgxpool.Pool, ownerTenant, publicURL string) *Recorder {
	host := ""
	if u, err := url.Parse(publicURL); err == nil {
		host = u.Hostname()
	}
	return &Recorder{Pool: pool, OwnerTenant: ownerTenant, Host: host, Now: time.Now}
}

// salt returns today's salt, cached per process until the day changes.
func (r *Recorder) todaysSalt(ctx context.Context, now time.Time) ([]byte, error) {
	day := now.UTC().Format(time.DateOnly)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.day == day && r.salt != nil {
		return r.salt, nil
	}
	salt, err := store.DailySalt(ctx, r.Pool, now)
	if err != nil {
		return nil, err
	}
	r.day, r.salt = day, salt
	return salt, nil
}

// Visitor is the day's pseudonym for an IP address and user agent.
func Visitor(salt []byte, ip, userAgent string) string {
	mac := hmac.New(sha256.New, salt)
	mac.Write([]byte(ip))
	mac.Write([]byte{0})
	mac.Write([]byte(userAgent))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

var botPattern = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|headless|preview|scan|monitor|lighthouse|curl|wget|python|go-http|java/|okhttp|facebookexternalhit|embedly|whatsapp|telegram|slack`)

// IsBot reports whether a user agent is an automated client (or missing).
func IsBot(userAgent string) bool {
	return strings.TrimSpace(userAgent) == "" || botPattern.MatchString(userAgent)
}

// Device is a coarse device class, from the user agent.
func Device(userAgent string) string {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "ipad") || strings.Contains(ua, "tablet") || (strings.Contains(ua, "android") && !strings.Contains(ua, "mobile")):
		return "tablet"
	case strings.Contains(ua, "mobi") || strings.Contains(ua, "iphone") || strings.Contains(ua, "android"):
		return "mobile"
	default:
		return "desktop"
	}
}

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,49}$`)
	refPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	eventNames  = map[string]bool{"demo_ready": true, "demo_open": true, "intro_skip": true}
)

// Page maps a URL path to the page it counts as, and the project it belongs to. Only the site's
// own public pages count; anything else is rejected, so arbitrary strings never reach the table.
func Page(path string) (page string, project *string, ok bool) {
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return "/", nil, true
	}
	switch path {
	case "/projects", "/about", "/status", "/privacy":
		return path, nil, true
	}
	for _, prefix := range []string{"/projects/", "/go/"} {
		if slug, found := strings.CutPrefix(path, prefix); found && slugPattern.MatchString(slug) {
			return path, &slug, true
		}
	}
	if strings.HasPrefix(path, "/status/incidents/") {
		return "/status/incidents", nil, true
	}
	return "", nil, false
}

// Ref normalizes a ?ref= tag; invalid tags are dropped.
func Ref(tag string) *string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if !refPattern.MatchString(tag) {
		return nil
	}
	return &tag
}

// Referrer keeps only the host of another site that linked here.
func (r *Recorder) Referrer(raw string) *string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host == strings.TrimPrefix(r.Host, "www.") || len(host) > 100 {
		return nil
	}
	return &host
}

// Hit is one page view as the browser reports it.
type Hit struct {
	ID        string
	Path      string
	Ref       string
	Referrer  string
	IP        string
	UserAgent string
}

// View records a page view. Views that shouldn't be counted are silently dropped; the error is
// only for storage failures.
func (r *Recorder) View(ctx context.Context, h Hit) error {
	page, project, ok := Page(h.Path)
	if !ok || IsBot(h.UserAgent) {
		return nil
	}
	now := r.Now()
	salt, err := r.todaysSalt(ctx, now)
	if err != nil {
		return err
	}
	v := store.PageView{
		ID: h.ID, At: now, Visitor: Visitor(salt, h.IP, h.UserAgent), Path: page, Project: project,
		Ref: Ref(h.Ref), Referrer: r.Referrer(h.Referrer), Device: Device(h.UserAgent),
	}
	return store.WithTenant(ctx, r.Pool, r.OwnerTenant, func(tx pgx.Tx) error { return store.InsertView(ctx, tx, r.OwnerTenant, v) })
}

// Ping credits a view with engaged time (measured on the server).
func (r *Recorder) Ping(ctx context.Context, viewID string) error {
	return store.WithTenant(ctx, r.Pool, r.OwnerTenant, func(tx pgx.Tx) error { return store.Ping(ctx, tx, viewID) })
}

// Event records something a visitor did on a view, such as opening a demo.
func (r *Recorder) Event(ctx context.Context, viewID, name string) error {
	if !eventNames[name] {
		return nil
	}
	return store.WithTenant(ctx, r.Pool, r.OwnerTenant, func(tx pgx.Tx) error { return store.InsertEvent(ctx, tx, r.OwnerTenant, viewID, name) })
}

// Report is the owner's private view of the last N days.
type Report struct {
	Since     time.Time           `json:"since"`
	Summary   store.Summary       `json:"summary"`
	Pages     []store.PageStat    `json:"pages"`
	Projects  []store.ProjectStat `json:"projects"`
	Refs      []store.RefStat     `json:"refs"`
	Referrers []store.Count       `json:"referrers"`
	Devices   []store.Count       `json:"devices"`
}

func (r *Recorder) Report(ctx context.Context, days int) (Report, error) {
	rep := Report{Since: r.Now().AddDate(0, 0, -days)}
	err := store.WithTenant(ctx, r.Pool, r.OwnerTenant, func(tx pgx.Tx) (err error) {
		if rep.Summary, err = store.SiteSummary(ctx, tx, rep.Since); err != nil {
			return err
		}
		if rep.Pages, err = store.PageStats(ctx, tx, rep.Since); err != nil {
			return err
		}
		if rep.Projects, err = store.ProjectStats(ctx, tx, rep.Since); err != nil {
			return err
		}
		if rep.Refs, err = store.RefStats(ctx, tx, rep.Since); err != nil {
			return err
		}
		if rep.Referrers, err = store.Breakdown(ctx, tx, "referrer", rep.Since); err != nil {
			return err
		}
		rep.Devices, err = store.Breakdown(ctx, tx, "device", rep.Since)
		return err
	})
	return rep, err
}

// MinPublic is the fewest visitors a public figure may describe. Smaller counts are shown only as
// "fewer than 5", so the public page can't be used to tell that one particular person visited.
const MinPublic = 5

// Readership is one project's public figures.
type Readership struct {
	Project       string
	Visitors      int // 0 when suppressed
	Suppressed    bool
	MedianMinutes float64
	Opens         int
}

// Public returns each project's readership over the last N days, suppressing small counts.
func (r *Recorder) Public(ctx context.Context, days int) ([]Readership, error) {
	var stats []store.ProjectStat
	err := store.WithTenant(ctx, r.Pool, r.OwnerTenant, func(tx pgx.Tx) (err error) {
		stats, err = store.ProjectStats(ctx, tx, r.Now().AddDate(0, 0, -days))
		return err
	})
	out := make([]Readership, 0, len(stats))
	for _, s := range stats {
		row := Readership{Project: s.Project}
		if s.Visitors < MinPublic {
			row.Suppressed = true
		} else {
			row.Visitors, row.MedianMinutes, row.Opens = s.Visitors, s.MedianEngaged/60, s.Opens
		}
		out = append(out, row)
	}
	return out, err
}
