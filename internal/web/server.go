// Package web serves Lighthouse over HTTP: the public status page (HTML rendered on the server),
// and the JSON API behind the console.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/time/rate"

	"github.com/MrtnOmwenga/lighthouse/internal/analytics"
	"github.com/MrtnOmwenga/lighthouse/internal/auth"
	"github.com/MrtnOmwenga/lighthouse/internal/config"
	"github.com/MrtnOmwenga/lighthouse/internal/monitor"
	"github.com/MrtnOmwenga/lighthouse/internal/oidc"
	"github.com/MrtnOmwenga/lighthouse/internal/site"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

type Server struct {
	Config      config.Config
	Pool        *pgxpool.Pool
	Auth        *auth.Service
	Log         *slog.Logger
	OwnerTenant string // whose monitors the public status page shows
	Now         func() time.Time
	Site        *site.Site      // the portfolio's content
	Readiness   *site.Readiness // wakes demos and reports when they're up
	Analytics   *analytics.Recorder

	// Tick and TickVerifier serve POST /internal/tick when checks are scheduled from outside.
	Tick         Ticker
	TickVerifier *oidc.Verifier
	// Drive, when checks are scheduled from outside, runs a tenant's due simulated checks as its
	// console reads data, so a sandbox is checked while someone is watching it.
	Drive func(ctx context.Context, tenantID string) int
	// Checks runs a check on request: "check now" on a monitor, and trying settings before they
	// are saved. Without it those endpoints don't exist.
	Checks *monitor.Scheduler

	pages  *template.Template
	status statusCache
	writes *limiter // API writes per client
}

func New(cfg config.Config, pool *pgxpool.Pool, authn *auth.Service, log *slog.Logger, ownerTenant string) *Server {
	if authn.Log == nil {
		authn.Log = log
	}
	return &Server{
		Config: cfg, Pool: pool, Auth: authn, Log: log, OwnerTenant: ownerTenant, Now: time.Now,
		Site:      &site.Site{Stories: map[string]*site.Story{}},
		Readiness: site.NewReadiness(monitor.NewProber()),
		Analytics: analytics.New(pool, ownerTenant, cfg.PublicURL),
		pages:     template.Must(template.New("").Funcs(funcs).ParseFS(templateFS, "templates/*.html")),
		writes:    newLimiter(rate.Every(200*time.Millisecond), 20),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheFor(time.Hour, http.FileServerFS(static))))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("POST /internal/tick", s.tick)
	// Public: the portfolio.
	mux.HandleFunc("GET /{$}", s.frontPage)
	mux.HandleFunc("GET /projects", s.projectsPage)
	mux.HandleFunc("GET /projects/{slug}", s.storyPage)
	mux.HandleFunc("GET /about", s.aboutPage)
	mux.HandleFunc("GET /privacy", s.privacyPage)
	mux.HandleFunc("GET /media/{name}", s.media)
	mux.HandleFunc("GET /robots.txt", s.robots)
	mux.HandleFunc("GET /sitemap.xml", s.sitemap)
	mux.HandleFunc("GET /go/{slug}", s.launchPage)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("GET /api/projects/{slug}/ready", s.projectReady)

	// Public: status.
	mux.HandleFunc("GET /status", s.statusPage)
	mux.HandleFunc("GET /status/incidents/{id}", s.incidentPage)
	mux.HandleFunc("GET /api/status", s.publicStatus)

	// Signing in and out.
	mux.HandleFunc("GET /auth/github", s.errs(s.Auth.GitHubStart))
	mux.HandleFunc("GET /auth/github/callback", s.githubCallback)
	mux.HandleFunc("POST /auth/dev", s.errs(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Auth.DevLogin(w, r); err != nil {
			return err
		}
		return writeJSON(w, http.StatusOK, map[string]string{"role": "owner"})
	}))
	mux.HandleFunc("POST /auth/logout", s.errs(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.Auth.Logout(w, r); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}))
	mux.HandleFunc("POST /api/sandbox", s.errs(s.createSandbox))
	mux.HandleFunc("POST /api/sandbox/reset", s.signedIn(s.resetSandbox))
	mux.HandleFunc("GET /api/me", s.signedIn(s.me))
	mux.HandleFunc("GET /api/sign-in-options", s.signInOptions)
	mux.HandleFunc("GET /api/session", s.session)
	mux.HandleFunc("GET /api/security", s.signedIn(s.security))
	mux.HandleFunc("DELETE /api/sessions/{id}", s.signedIn(s.endSession))
	mux.HandleFunc("POST /api/sessions/end-others", s.signedIn(s.endOtherSessions))

	// The console: a single-page app, embedded in the binary.
	mux.Handle("GET /console/", s.console())
	mux.HandleFunc("GET /console", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/console/", http.StatusMovedPermanently)
	})

	// Visit analytics: collection is public; the report is the owner's alone.
	mux.HandleFunc("POST /api/a/view", s.analyticsView)
	mux.HandleFunc("POST /api/a/ping", s.analyticsPing)
	mux.HandleFunc("POST /api/a/event", s.analyticsEvent)
	mux.HandleFunc("GET /api/analytics", s.signedIn(s.analyticsReport))
	mux.HandleFunc("GET /api/analytics/exclusion", s.signedIn(s.exclusion))
	mux.HandleFunc("PUT /api/analytics/exclusion", s.signedIn(s.setExclusion))

	// The console API: every query runs inside the caller's tenant.
	mux.HandleFunc("GET /api/my-status", s.signedIn(s.myStatus))
	mux.HandleFunc("GET /api/monitors", s.signedIn(s.listMonitors))
	mux.HandleFunc("POST /api/monitors", s.signedIn(s.createMonitor))
	mux.HandleFunc("POST /api/monitors/test", s.signedIn(s.testMonitor))
	mux.HandleFunc("GET /api/monitors/{id}", s.signedIn(s.getMonitor))
	mux.HandleFunc("POST /api/monitors/{id}/check", s.signedIn(s.checkMonitor))
	mux.HandleFunc("GET /api/overview", s.signedIn(s.overview))
	mux.HandleFunc("PUT /api/monitors/{id}", s.signedIn(s.updateMonitor))
	mux.HandleFunc("DELETE /api/monitors/{id}", s.signedIn(s.deleteMonitor))
	mux.HandleFunc("GET /api/monitors/{id}/checks", s.signedIn(s.listChecks))
	mux.HandleFunc("PUT /api/monitors/{id}/mode", s.signedIn(s.setMode))
	mux.HandleFunc("GET /api/incidents", s.signedIn(s.listIncidents))
	mux.HandleFunc("POST /api/incidents", s.signedIn(s.createIncident))
	mux.HandleFunc("GET /api/incidents/{id}", s.signedIn(s.getIncident))
	mux.HandleFunc("PATCH /api/incidents/{id}", s.signedIn(s.updateIncident))
	mux.HandleFunc("POST /api/incidents/{id}/comments", s.signedIn(s.addComment))

	return withRequestID(s.recoverer(s.logRequests(s.edgeOnly(securityHeaders(s.Config, s.sameOrigin(s.limitWrites(s.Auth.Middleware(mux))))))))
}

type requestIDKey struct{}

// withRequestID names each request, in the X-Request-Id response header and in its log lines, so
// an error someone reports can be traced to what the server recorded about it.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw [8]byte
		_, _ = rand.Read(raw[:])
		id := hex.EncodeToString(raw[:])
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Pool.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	err := s.Auth.GitHubCallback(w, r)
	switch {
	case err == nil:
		http.Redirect(w, r, "/console/", http.StatusFound)
	case errors.Is(err, auth.ErrNotOwner):
		s.renderError(w, http.StatusForbidden, "This is Martin's console", "Only the owner can sign in here. You can still look around: the status page is public.")
	default:
		s.Auth.Security("sign_in_failed", "err", err.Error())
		s.renderError(w, http.StatusBadRequest, "Sign-in failed", "GitHub sign-in didn't complete. Please try again.")
	}
}

// createSandbox starts a sandbox, or returns to the one this browser already has: asking twice
// doesn't leave an abandoned tenant behind, and doesn't use up the visitor's allowance.
func (s *Server) createSandbox(w http.ResponseWriter, r *http.Request) error {
	if id, ok := auth.FromContext(r.Context()); ok && !id.Owner() {
		left := int(time.Until(id.Expires).Minutes())
		return writeJSON(w, http.StatusOK, map[string]any{"role": "sandbox", "expiresInMinutes": max(left, 0), "resumed": true})
	}
	allowed, err := s.sandboxAllowed(r)
	if err != nil {
		return err
	}
	if !allowed {
		s.Auth.Security("rate_limited", "what", "sandbox")
		return errTooMany
	}
	if err := s.Auth.Sandbox(w, r); err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, map[string]any{"role": "sandbox", "expiresInMinutes": int(s.Config.SandboxTTL.Minutes())})
}

// resetSandbox puts a sandbox back to how it started: its monitors and incidents are removed and
// the sample monitors added again. The session, and so the time left, stays as it was.
func (s *Server) resetSandbox(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	if id.Owner() {
		return errForbidden
	}
	err := s.inTenant(r, id, func(tx pgx.Tx) error {
		if err := store.ClearTenant(r.Context(), tx); err != nil {
			return err
		}
		return auth.SeedSandbox(r.Context(), tx, id.TenantID)
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	return writeJSON(w, http.StatusOK, map[string]any{"role": id.Role, "login": id.Login, "expiresAt": id.Expires})
}

// session says who is signed in, or that nobody is, without treating "nobody" as an error.
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := auth.FromContext(r.Context())
	if !ok {
		_ = writeJSON(w, http.StatusOK, map[string]any{"signedIn": false})
		return
	}
	_ = writeJSON(w, http.StatusOK, map[string]any{"signedIn": true, "role": id.Role, "login": id.Login, "expiresAt": id.Expires})
}

// signInOptions tells the console which ways of signing in exist here.
func (s *Server) signInOptions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	_ = writeJSON(w, http.StatusOK, map[string]bool{
		"github":  s.Config.GitHubClientID != "",
		"dev":     s.Config.DevLogin && !s.Config.Production(),
		"sandbox": true,
	})
}

// signedIn wraps a handler that needs a session.
func (s *Server) signedIn(h func(http.ResponseWriter, *http.Request, auth.Identity) error) http.HandlerFunc {
	return s.errs(func(w http.ResponseWriter, r *http.Request) error {
		id, ok := auth.FromContext(r.Context())
		if !ok {
			return errUnauthenticated
		}
		if s.Drive != nil && r.Method == http.MethodGet {
			ctx, cancel := context.WithTimeout(r.Context(), driveTimeout)
			s.Drive(ctx, id.TenantID)
			cancel()
		}
		return h(w, r, id)
	})
}

// driveTimeout bounds the checks a console read may wait for; anything unfinished is picked up by
// the next read.
const driveTimeout = 3 * time.Second

// inTenant runs fn in a transaction scoped to the caller's tenant.
func (s *Server) inTenant(r *http.Request, id auth.Identity, fn func(pgx.Tx) error) error {
	return store.WithTenant(r.Context(), s.Pool, id.TenantID, fn)
}

// Errors, as RFC 9457 problem details.

type problem struct {
	status int
	title  string
	detail string
	fields []fieldError // for an invalid request: what is wrong with which field
}

// fieldError is one problem with one field of a request, named as the request names it.
type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (p problem) Error() string { return p.title }

var (
	errUnauthenticated = problem{status: http.StatusUnauthorized, title: "Sign in first"}
	errForbidden       = problem{status: http.StatusForbidden, title: "Not allowed"}
	errNotFound        = problem{status: http.StatusNotFound, title: "Not found"}
	errTooMany         = problem{status: http.StatusTooManyRequests, title: "Too many requests", detail: "Slow down and try again shortly."}
)

func invalid(detail string) problem {
	return problem{status: http.StatusUnprocessableEntity, title: "Invalid request", detail: detail}
}

// invalidFields is an invalid request with every problem listed by field. The detail repeats them
// in one line, for clients that only show that.
func invalidFields(fields []fieldError) problem {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = f.Field + ": " + f.Message
	}
	return problem{status: http.StatusUnprocessableEntity, title: "Invalid request", detail: strings.Join(parts, " "), fields: fields}
}

func (s *Server) errs(h func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err == nil {
			return
		}
		var p problem
		var pg *pgconn.PgError
		switch {
		case errors.As(err, &p):
		case errors.Is(err, store.ErrNotFound):
			p = errNotFound
		case errors.Is(err, auth.ErrForbidden):
			p = errForbidden
		case errors.As(err, &pg) && pg.Code == "23505":
			p = problem{status: http.StatusConflict, title: "Already exists", detail: "That slug is already in use.",
				fields: []fieldError{{"slug", "Already in use."}}}
		case errors.As(err, &pg) && (pg.Code == "23514" || pg.Code == "22001"):
			p = invalid("A value is outside its allowed range.")
		case errors.Is(err, context.Canceled):
			return
		default:
			s.Log.Error("request failed", "id", requestID(r), "method", r.Method, "path", r.URL.Path, "err", err)
			p = problem{status: http.StatusInternalServerError, title: "Something went wrong"}
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(p.status)
		body := map[string]any{"status": p.status, "title": p.title}
		if p.detail != "" {
			body["detail"] = p.detail
		}
		if len(p.fields) > 0 {
			body["errors"] = p.fields
		}
		if p.status >= 500 {
			// What the visitor can quote, and what finds the log line that explains it.
			body["requestId"] = requestID(r)
		}
		_ = json.NewEncoder(w).Encode(body)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// readJSON decodes a small JSON body strictly: unknown fields and trailing data are errors.
func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalid("The body must be a JSON object with the documented fields.")
	}
	if dec.More() {
		return invalid("The body must hold a single JSON object.")
	}
	return nil
}

// Middleware.

func securityHeaders(cfg config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; img-src 'self' data:; font-src 'self'; script-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if cfg.SecureCookies() {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin refuses state-changing requests from other sites. SameSite=Lax cookies already stop
// most cross-site requests; this also covers same-site subdomains and older browsers.
func (s *Server) sameOrigin(next http.Handler) http.Handler {
	public, _ := url.Parse(s.Config.PublicURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			origin := r.Header.Get("Origin")
			site := r.Header.Get("Sec-Fetch-Site")
			allowed := false
			if origin != "" {
				o, err := url.Parse(origin)
				allowed = err == nil && o.Scheme == public.Scheme && o.Host == public.Host
			} else {
				// No Origin: not a modern browser's cross-site request (e.g. curl), unless the
				// browser says otherwise.
				allowed = site == "" || site == "same-origin" || site == "none"
			}
			if !allowed {
				s.errs(func(http.ResponseWriter, *http.Request) error { return errForbidden })(w, r)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) limitWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.writes.allow(s.clientIP(r)) {
			s.Auth.Security("rate_limited", "what", "writes", "path", r.URL.Path)
			s.errs(func(http.ResponseWriter, *http.Request) error { return errTooMany })(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// edgeOnly refuses requests that didn't come through the edge proxy, when one is configured (see
// config.EdgeSecret). Health checks (the platform probes the container directly) and the tick (it
// carries its own proof of identity) are exempt.
func (s *Server) edgeOnly(next http.Handler) http.Handler {
	secret := []byte(s.Config.EdgeSecret)
	if len(secret) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz", "/readyz", "/internal/tick":
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Edge-Secret")), secret) != 1 {
			s.Auth.Security("edge_bypassed", "path", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) clientIP(r *http.Request) string {
	if h := s.Config.ClientIPHeader; h != "" {
		if v := strings.TrimSpace(strings.Split(r.Header.Get(h), ",")[0]); v != "" {
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// logRequests logs method, path (never the query, which can carry OAuth codes), status and time.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			return
		}
		s.Log.Info("request", "id", requestID(r), "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.Log.Error("panic", "id", requestID(r), "path", r.URL.Path, "panic", v)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func cacheFor(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(d.Seconds())))
		next.ServeHTTP(w, r)
	})
}
