// Package auth signs people in. There are two kinds of session:
//
//   - owner: the one GitHub account named by OWNER_GITHUB_ID, via GitHub OAuth. Everyone else who
//     signs in with GitHub is turned away. (Outside production, DEV_LOGIN allows an owner session
//     without GitHub.)
//   - sandbox: anyone, no account, a throwaway tenant with sample monitors that expires.
//
// A session is a random token in an HttpOnly cookie; the database stores only its SHA-256 hash, so
// a leaked sessions table can't be replayed.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrtnOmwenga/lighthouse/internal/analytics"
	"github.com/MrtnOmwenga/lighthouse/internal/config"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
)

const OwnerTTL = 12 * time.Hour

// Cookie names. Over HTTPS they carry the __Host- prefix: a browser then accepts the cookie only
// from this exact host, over HTTPS, for the whole site. Without it, a page on a sibling address
// (the demos share the parent domain) could set a cookie of the same name for the whole domain
// and so plant a session in a visitor's browser. Browsers refuse the prefix over plain HTTP, so
// local development uses the bare names.
const (
	sessionCookie = "lh_session"
	stateCookie   = "lh_oauth_state"
	hostPrefix    = "__Host-"
)

func (s *Service) cookieName(name string) string {
	if s.Config.SecureCookies() {
		return hostPrefix + name
	}
	return name
}

// SessionCookieName is the session cookie's name in this deployment.
func (s *Service) SessionCookieName() string { return s.cookieName(sessionCookie) }

// CurrentTokenHash is the stored hash of the request's session token, or "" without one.
func (s *Service) CurrentTokenHash(r *http.Request) string {
	c, err := r.Cookie(s.SessionCookieName())
	if err != nil || c.Value == "" {
		return ""
	}
	return hashToken(c.Value)
}

// Identity is who is making a request.
type Identity struct {
	TenantID string
	Role     string // owner or sandbox
	Login    string // GitHub login for the owner; empty for sandboxes
	Expires  time.Time
}

func (i Identity) Owner() bool { return i.Role == "owner" }

type Service struct {
	Pool   *pgxpool.Pool
	Config config.Config
	Client *http.Client // for GitHub; a short timeout
	// Log, if set, hears each security event as a structured line (msg "security", with an
	// "event"), which is what log-based alerts match on.
	Log *slog.Logger
}

// Security logs a security event: a sign-in, a refusal, a rejected caller.
func (s *Service) Security(event string, attrs ...any) {
	if s != nil && s.Log != nil {
		s.Log.Warn("security", append([]any{"event", event}, attrs...)...)
	}
}

// record adds to the owner's record of sign-ins and logs the event. A failure to record is
// logged and doesn't undo what happened.
func (s *Service) record(ctx context.Context, e store.AuthEvent) {
	attrs := []any{"login", e.Login}
	if e.GitHubID != nil {
		attrs = append(attrs, "github_id", *e.GitHubID)
	}
	s.Security(e.Kind, attrs...)
	tenant, err := store.OwnerTenant(ctx, s.Pool, s.Config.OwnerName)
	if err == nil {
		err = store.WithTenant(ctx, s.Pool, tenant, func(tx pgx.Tx) error { return store.AddAuthEvent(ctx, tx, tenant, e) })
	}
	if err != nil && s.Log != nil {
		s.Log.Error("recording a sign-in event", "err", err)
	}
}

func New(pool *pgxpool.Pool, cfg config.Config) *Service {
	return &Service{Pool: pool, Config: cfg, Client: &http.Client{Timeout: 10 * time.Second}}
}

type ctxKey struct{}

// FromContext returns the request's identity, if it has a valid session.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never fails (crypto/rand panics rather than return an error on Linux)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Middleware attaches the identity of a valid session cookie to the request context. Requests
// without one carry on anonymously; handlers decide what that allows.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hash := s.CurrentTokenHash(r); hash != "" {
			sess, err := store.LookupSession(r.Context(), s.Pool, hash)
			if err == nil && time.Now().Before(sess.ExpiresAt) {
				id := Identity{TenantID: sess.TenantID, Role: sess.Role, Expires: sess.ExpiresAt}
				if sess.GitHubLogin != nil {
					id.Login = *sess.GitHubLogin
				}
				r = r.WithContext(WithIdentity(r.Context(), id))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// startSession stores a new session and sets its cookie.
func (s *Service) startSession(w http.ResponseWriter, ctx context.Context, sess store.Session) error {
	token := randomToken()
	if err := store.WithTenant(ctx, s.Pool, sess.TenantID, func(tx pgx.Tx) error {
		return store.CreateSession(ctx, tx, hashToken(token), sess)
	}); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: s.SessionCookieName(), Value: token, Path: "/", Expires: sess.ExpiresAt,
		HttpOnly: true, Secure: s.Config.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Logout ends the current session.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) error {
	if hash := s.CurrentTokenHash(r); hash != "" {
		if id, ok := FromContext(r.Context()); ok {
			if err := store.WithTenant(r.Context(), s.Pool, id.TenantID, func(tx pgx.Tx) error {
				return store.DeleteSession(r.Context(), tx, hash)
			}); err != nil {
				return err
			}
			if id.Owner() {
				s.record(r.Context(), store.AuthEvent{Kind: "signed_out", Login: id.Login})
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: s.SessionCookieName(), Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Config.SecureCookies(), SameSite: http.SameSiteLaxMode})
	return nil
}

// DevLogin starts an owner session without GitHub. Only available when DEV_LOGIN is on, which
// configuration refuses in production; checked again here.
func (s *Service) DevLogin(w http.ResponseWriter, r *http.Request) error {
	if !s.Config.DevLogin || s.Config.Production() {
		return ErrForbidden
	}
	return s.ownerSession(w, r, "dev", nil)
}

func (s *Service) ownerSession(w http.ResponseWriter, r *http.Request, login string, githubID *int64) error {
	ctx := r.Context()
	tenant, err := store.OwnerTenant(ctx, s.Pool, s.Config.OwnerName)
	if err != nil {
		return err
	}
	if err := s.startSession(w, ctx, store.Session{TenantID: tenant, Role: "owner", GitHubLogin: &login,
		ExpiresAt: time.Now().Add(OwnerTTL), Device: analytics.Device(r.UserAgent())}); err != nil {
		return err
	}
	s.record(ctx, store.AuthEvent{Kind: "signed_in", Login: login, GitHubID: githubID, Detail: analytics.Device(r.UserAgent())})
	return nil
}

var (
	ErrForbidden = errors.New("forbidden")
	ErrNotOwner  = errors.New("that GitHub account isn't the owner")
	ErrOAuth     = errors.New("GitHub sign-in failed")
)

func (s *Service) redirectURI() string { return s.Config.PublicURL + "/auth/github/callback" }

// GitHubStart sends the browser to GitHub, with a random state bound to this browser by a
// short-lived cookie (so a sign-in can't be started on someone else's behalf).
func (s *Service) GitHubStart(w http.ResponseWriter, r *http.Request) error {
	if s.Config.GitHubClientID == "" {
		return ErrForbidden
	}
	state := randomToken()
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(stateCookie), Value: state, Path: "/", MaxAge: 600,
		HttpOnly: true, Secure: s.Config.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
	q := url.Values{"client_id": {s.Config.GitHubClientID}, "redirect_uri": {s.redirectURI()}, "state": {state}, "allow_signup": {"false"}}
	http.Redirect(w, r, s.Config.GitHubOAuthBase+"/login/oauth/authorize?"+q.Encode(), http.StatusFound)
	return nil
}

// GitHubCallback finishes the sign-in: checks the state, exchanges the code, asks GitHub who the
// user is and admits only the owner. No scopes are requested, and GitHub's token is discarded
// once the user's ID is known.
func (s *Service) GitHubCallback(w http.ResponseWriter, r *http.Request) error {
	c, err := r.Cookie(s.cookieName(stateCookie))
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(stateCookie), Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Config.SecureCookies(), SameSite: http.SameSiteLaxMode})
	state := r.URL.Query().Get("state")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		return fmt.Errorf("%w: state mismatch", ErrOAuth)
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		return fmt.Errorf("%w: no code", ErrOAuth)
	}
	token, err := s.exchange(r.Context(), code)
	if err != nil {
		return err
	}
	user, err := s.githubUser(r.Context(), token)
	if err != nil {
		return err
	}
	if s.Config.OwnerGitHubID == 0 || user.ID != s.Config.OwnerGitHubID {
		s.record(r.Context(), store.AuthEvent{Kind: "refused", Login: user.Login, GitHubID: &user.ID, Detail: "not the owner's account"})
		return ErrNotOwner
	}
	return s.ownerSession(w, r, user.Login, &user.ID)
}

func (s *Service) exchange(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"client_id": {s.Config.GitHubClientID}, "client_secret": {s.Config.GitHubClientSecret},
		"code": {code}, "redirect_uri": {s.redirectURI()},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Config.GitHubOAuthBase+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := s.doJSON(req, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("%w: %s", ErrOAuth, out.Error)
	}
	return out.AccessToken, nil
}

type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

func (s *Service) githubUser(ctx context.Context, token string) (githubUser, error) {
	var u githubUser
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Config.GitHubAPIBase+"/user", nil)
	if err != nil {
		return u, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if err := s.doJSON(req, &u); err != nil {
		return u, err
	}
	if u.ID == 0 {
		return u, fmt.Errorf("%w: no user", ErrOAuth)
	}
	return u, nil
}

func (s *Service) doJSON(req *http.Request, out any) error {
	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOAuth, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: GitHub answered %d", ErrOAuth, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(out); err != nil {
		return fmt.Errorf("%w: %v", ErrOAuth, err)
	}
	return nil
}

// Sandbox creates a throwaway tenant seeded with sample monitors and signs the visitor into it.
func (s *Service) Sandbox(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	tenant := uuid.NewString()
	if err := store.CreateSandbox(ctx, s.Pool, tenant, func(tx pgx.Tx) error { return SeedSandbox(ctx, tx, tenant) }); err != nil {
		return err
	}
	s.Security("sandbox_created")
	return s.startSession(w, ctx, store.Session{TenantID: tenant, Role: "sandbox", ExpiresAt: time.Now().Add(s.Config.SandboxTTL), Device: analytics.Device(r.UserAgent())})
}

// SeedSandbox adds the sample monitors a sandbox starts with: simulated sites, so a visitor can
// break and fix them without Lighthouse sending traffic anywhere.
func SeedSandbox(ctx context.Context, tx pgx.Tx, tenant string) error {
	for _, m := range []struct{ name, slug, mode string }{
		{"Storefront", "storefront", "up"},
		{"Checkout API", "checkout-api", "flaky"},
		{"Search", "search", "slow"},
	} {
		if _, err := store.CreateMonitor(ctx, tx, tenant, store.MonitorInput{
			Name: m.name, Slug: m.slug, Kind: "simulated", SimulatedMode: m.mode, IntervalSeconds: 10, TimeoutMS: 5000,
			ExpectedStatusMin: 200, ExpectedStatusMax: 299, FailureThreshold: 2, RecoveryThreshold: 2, Public: true,
		}); err != nil {
			return err
		}
	}
	return nil
}
