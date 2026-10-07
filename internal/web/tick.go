package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/MrtnOmwenga/lighthouse/internal/oidc"
)

// Ticker runs one round of scheduled work: the checks that are due, and housekeeping when it's
// due. It returns how many checks it ran.
type Ticker func(ctx context.Context) (int, error)

// tickTimeout bounds one round: a probe takes at most 30 s, and the caller (Cloud Scheduler) gives
// up after its own deadline, so there's no point running longer.
const tickTimeout = 150 * time.Second

// tick serves POST /internal/tick, for deployments where Lighthouse can't run its own clock
// (Cloud Run gives an idle instance no CPU). Only a Google-issued identity token for the
// configured audience and service account gets in; anything else is a 404, so the endpoint
// doesn't advertise itself.
func (s *Server) tick(w http.ResponseWriter, r *http.Request) {
	if s.Tick == nil || s.TickVerifier == nil {
		http.NotFound(w, r)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.TickVerifier.Verify(r.Context(), token); err != nil {
		if errors.Is(err, oidc.ErrInvalid) {
			s.Auth.Security("tick_rejected", "err", err.Error())
			http.NotFound(w, r)
			return
		}
		// The signing keys couldn't be fetched: a transient failure, worth a retry.
		s.Log.Error("tick: verifying the caller", "err", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), tickTimeout)
	defer cancel()
	n, err := s.Tick(ctx)
	if err != nil {
		s.Log.Error("tick", "err", err)
		http.Error(w, "tick failed", http.StatusInternalServerError)
		return
	}
	_ = writeJSON(w, http.StatusOK, map[string]int{"checked": n})
}
