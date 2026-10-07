// Package config reads Lighthouse's settings from the environment and refuses to start with an
// invalid or unsafe combination, listing every problem at once.
package config

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env         string // development, test or production
	Addr        string // listen address, e.g. ":8080"
	PublicURL   string // how browsers reach Lighthouse; used for OAuth redirects and origin checks
	DatabaseURL string // the API's least-privilege role (row-level security applies)

	GitHubClientID     string
	GitHubClientSecret string
	OwnerGitHubID      int64  // the only GitHub account that may sign in as the owner
	GitHubOAuthBase    string // overridable so tests can stand in for GitHub
	GitHubAPIBase      string

	// DevLogin enables POST /auth/dev, an owner login without GitHub, for local development only.
	DevLogin bool

	// ClientIPHeader names the header a trusted reverse proxy puts the visitor's IP in (e.g.
	// CF-Connecting-IP behind Cloudflare), for rate limiting. Empty: use the connection's address.
	// Only set it when every request passes through that proxy, or the header can be forged.
	ClientIPHeader string
	// EdgeSecret, when set, is a value only the edge proxy (a Cloudflare Worker) sends, in the
	// X-Edge-Secret header. Requests without it are refused, apart from health checks and the
	// scheduler's tick: the origin (a public *.run.app URL) can't be used to bypass the edge, and
	// ClientIPHeader can't be forged by calling it directly.
	EdgeSecret string

	// Email alerts when the owner's monitors open or resolve an incident. Off unless AlertTo is set.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	AlertFrom    string
	AlertTo      []string

	// SiteDir holds the portfolio's content: site.yaml, stories/ and media/. Empty: no content.
	SiteDir string

	OwnerName     string        // shown on the public status page
	CheckWorkers  int           // concurrent checks
	SandboxTTL    time.Duration // how long a visitor's sandbox lives
	SandboxLimit  int           // new sandboxes per client per hour
	RetentionDays int           // how long check results are kept

	// StatusCache is how long the public status is reused before it is built again; every public
	// page shows it. Zero builds it for each request.
	StatusCache time.Duration
	// Schedule is how checks get run. "loop" (the default): Lighthouse runs its own clock, which
	// needs a process that's always running. "external": something else calls POST /internal/tick
	// (Cloud Scheduler, on Cloud Run, where an idle instance gets no CPU); each call runs the checks
	// that are due.
	Schedule string
	// TickAudience and TickCaller: the identity token /internal/tick requires in external mode,
	// issued by Google for this audience to this service account, and to no one else.
	TickAudience string
	TickCaller   string
	// TickSlack: in external mode, how far ahead of its time a monitor counts as due (never more
	// than a tenth of its interval), so calls that arrive a little early don't skip it.
	TickSlack time.Duration
	// MetricsPushURL, with a user and token: where a round of checks reports its figures when it
	// ends (InfluxDB line protocol, as Grafana Cloud accepts). Empty: nothing is sent.
	MetricsPushURL, MetricsPushUser, MetricsPushToken string
	// ConfirmAfter: how soon to re-check an HTTP monitor whose state has started to change, so an
	// outage is confirmed in about a minute. Zero waits for the monitor's next scheduled check.
	ConfirmAfter time.Duration
	// WarmAbove: a passing HTTP check slower than this is taken to have woken a sleeping service,
	// recorded as a warm-up, and repeated at once. Zero records every check as it comes.
	WarmAbove time.Duration
}

// ExternalSchedule reports whether checks are driven by calls to /internal/tick.
func (c Config) ExternalSchedule() bool { return c.Schedule == "external" }

func (c Config) Production() bool { return c.Env == "production" }

// Load reads the configuration from getenv (os.Getenv in production, a map in tests).
func Load(getenv func(string) string) (Config, error) {
	var problems []string
	get := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}
	integer := func(key string, fallback, lo, hi int) int {
		raw := get(key, strconv.Itoa(fallback))
		n, err := strconv.Atoi(raw)
		if err != nil || n < lo || n > hi {
			problems = append(problems, fmt.Sprintf("%s must be a whole number between %d and %d", key, lo, hi))
		}
		return n
	}

	c := Config{
		Env:                get("LIGHTHOUSE_ENV", "development"),
		Addr:               get("ADDR", ":"+get("PORT", "8080")), // PORT: what Cloud Run sets
		PublicURL:          strings.TrimRight(get("PUBLIC_URL", "http://localhost:8080"), "/"),
		DatabaseURL:        get("DATABASE_URL", ""),
		GitHubClientID:     get("GITHUB_CLIENT_ID", ""),
		GitHubClientSecret: get("GITHUB_CLIENT_SECRET", ""),
		GitHubOAuthBase:    strings.TrimRight(get("GITHUB_OAUTH_BASE", "https://github.com"), "/"),
		GitHubAPIBase:      strings.TrimRight(get("GITHUB_API_BASE", "https://api.github.com"), "/"),
		DevLogin:           get("DEV_LOGIN", "false") == "true",
		ClientIPHeader:     get("CLIENT_IP_HEADER", ""),
		EdgeSecret:         get("EDGE_SECRET", ""),
		SiteDir:            get("SITE_DIR", ""),
		SMTPHost:           get("SMTP_HOST", ""),
		SMTPPort:           integer("SMTP_PORT", 587, 1, 65535),
		SMTPUsername:       get("SMTP_USERNAME", ""),
		SMTPPassword:       get("SMTP_PASSWORD", ""),
		AlertFrom:          get("ALERT_FROM", ""),
		OwnerName:          get("OWNER_NAME", "Lighthouse"),
		CheckWorkers:       integer("CHECK_WORKERS", 8, 1, 256),
		SandboxTTL:         time.Duration(integer("SANDBOX_TTL_MINUTES", 120, 5, 24*60)) * time.Minute,
		SandboxLimit:       integer("SANDBOX_LIMIT_PER_HOUR", 6, 1, 100000),
		RetentionDays:      integer("RETENTION_DAYS", 90, 1, 3650),
		StatusCache:        time.Duration(integer("STATUS_CACHE_SECONDS", 10, 0, 300)) * time.Second,
		Schedule:           get("SCHEDULE", "loop"),
		TickCaller:         get("TICK_CALLER", ""),
		TickSlack:          time.Duration(integer("TICK_SLACK_SECONDS", 60, 0, 600)) * time.Second,
		MetricsPushURL:     get("METRICS_PUSH_URL", ""),
		MetricsPushUser:    get("METRICS_PUSH_USER", ""),
		MetricsPushToken:   get("METRICS_PUSH_TOKEN", ""),
		ConfirmAfter:       time.Duration(integer("CONFIRM_SECONDS", 0, 0, 60)) * time.Second,
		WarmAbove:          time.Duration(integer("WARM_THRESHOLD_MS", 0, 0, 30000)) * time.Millisecond,
	}
	c.TickAudience = get("TICK_AUDIENCE", c.PublicURL+"/internal/tick")
	if owner := get("OWNER_GITHUB_ID", "0"); owner != "0" {
		id, err := strconv.ParseInt(owner, 10, 64)
		if err != nil || id <= 0 {
			problems = append(problems, "OWNER_GITHUB_ID must be your numeric GitHub user ID")
		}
		c.OwnerGitHubID = id
	}

	if to := get("ALERT_TO", ""); to != "" {
		for _, addr := range strings.Split(to, ",") {
			parsed, err := mail.ParseAddress(strings.TrimSpace(addr))
			if err != nil {
				problems = append(problems, "ALERT_TO must be a comma-separated list of email addresses")
				break
			}
			c.AlertTo = append(c.AlertTo, parsed.Address)
		}
		if c.SMTPHost == "" {
			problems = append(problems, "ALERT_TO needs SMTP_HOST (and usually SMTP_USERNAME and SMTP_PASSWORD)")
		}
		if _, err := mail.ParseAddress(c.AlertFrom); err != nil {
			problems = append(problems, "ALERT_TO needs ALERT_FROM, the address alerts come from")
		}
	}

	if c.EdgeSecret != "" && len(c.EdgeSecret) < 32 {
		problems = append(problems, "EDGE_SECRET must be at least 32 characters")
	}

	switch c.Schedule {
	case "loop":
	case "external":
		if !strings.HasSuffix(c.TickCaller, ".gserviceaccount.com") {
			problems = append(problems, "SCHEDULE=external needs TICK_CALLER, the service account allowed to call /internal/tick")
		}
	default:
		problems = append(problems, "SCHEDULE must be loop or external")
	}

	switch c.Env {
	case "development", "test", "production":
	default:
		problems = append(problems, "LIGHTHOUSE_ENV must be development, test or production")
	}
	if c.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is required")
	}
	if u, err := url.Parse(c.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		problems = append(problems, "PUBLIC_URL must be an absolute http(s) URL")
	}
	if c.DevLogin && c.Production() {
		problems = append(problems, "DEV_LOGIN must not be enabled in production")
	}
	if c.Production() {
		if !strings.HasPrefix(c.PublicURL, "https://") {
			problems = append(problems, "PUBLIC_URL must use https in production")
		}
		if c.GitHubClientID == "" || c.GitHubClientSecret == "" || c.OwnerGitHubID == 0 {
			problems = append(problems, "production needs GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET and OWNER_GITHUB_ID for the owner login")
		}
	}
	if len(problems) > 0 {
		return Config{}, errors.New("invalid configuration:\n  " + strings.Join(problems, "\n  "))
	}
	return c, nil
}

// Domain is the host of the public URL, without a port: what ${DOMAIN} means in the site content.
func (c Config) Domain() string {
	u, err := url.Parse(c.PublicURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// SecureCookies reports whether cookies should be marked Secure (served over HTTPS).
func (c Config) SecureCookies() bool { return strings.HasPrefix(c.PublicURL, "https://") }
