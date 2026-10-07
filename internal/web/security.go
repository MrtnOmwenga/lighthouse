package web

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MrtnOmwenga/lighthouse/internal/analytics"
	"github.com/MrtnOmwenga/lighthouse/internal/auth"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
)

// sandboxAllowed counts this visitor's new sandbox against their hourly allowance. The count is
// kept in the database, so it survives the instance sleeping and is shared between instances. The
// visitor is identified the way a page view is: a keyed hash of the address under the day's
// salt, so no address is stored.
func (s *Server) sandboxAllowed(r *http.Request) (bool, error) {
	now := s.Now().UTC()
	salt, err := store.DailySalt(r.Context(), s.Pool, now)
	if err != nil {
		return false, err
	}
	key := "sandbox:" + analytics.Visitor(salt, s.clientIP(r), "")
	return store.RateHit(r.Context(), s.Pool, key, time.Hour, max(s.Config.SandboxLimit, 1))
}

// security is the owner's view of who can act as him and who has tried: his live sessions and
// the recent record of sign-ins.
func (s *Server) security(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	if !id.Owner() {
		return errForbidden
	}
	var sessions []store.SessionInfo
	var events []store.AuthEvent
	err := s.inTenant(r, id, func(tx pgx.Tx) (err error) {
		if sessions, err = store.ListSessions(r.Context(), tx, s.Auth.CurrentTokenHash(r)); err != nil {
			return err
		}
		events, err = store.RecentAuthEvents(r.Context(), tx, 50)
		return err
	})
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions, "events": events})
}

// endSession ends one of the owner's sessions. Ending the current one is signing out; use that.
func (s *Server) endSession(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	if !id.Owner() {
		return errForbidden
	}
	sid, err := pathID(r)
	if err != nil {
		return err
	}
	err = s.inTenant(r, id, func(tx pgx.Tx) error {
		if err := store.EndSession(r.Context(), tx, sid); err != nil {
			return err
		}
		return store.AddAuthEvent(r.Context(), tx, id.TenantID, store.AuthEvent{Kind: "sessions_ended", Login: id.Login, Detail: "one session"})
	})
	if err != nil {
		return err
	}
	s.Auth.Security("sessions_ended", "login", id.Login, "count", 1)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// endOtherSessions signs the owner out everywhere except here.
func (s *Server) endOtherSessions(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	if !id.Owner() {
		return errForbidden
	}
	var n int64
	err := s.inTenant(r, id, func(tx pgx.Tx) (err error) {
		if n, err = store.EndOtherSessions(r.Context(), tx, s.Auth.CurrentTokenHash(r)); err != nil || n == 0 {
			return err
		}
		return store.AddAuthEvent(r.Context(), tx, id.TenantID, store.AuthEvent{Kind: "sessions_ended", Login: id.Login, Detail: "every other session"})
	})
	if err != nil {
		return err
	}
	if n > 0 {
		s.Auth.Security("sessions_ended", "login", id.Login, "count", n)
	}
	return writeJSON(w, http.StatusOK, map[string]int64{"ended": n})
}
