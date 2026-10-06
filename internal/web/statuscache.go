package web

import (
	"context"
	"sync"
	"time"

	"github.com/MrtnOmwenga/lighthouse/internal/status"
)

// statusCache keeps the owner's public status for a few seconds. Every public page shows it, so
// without this each page view runs the same queries; and when the database can't be reached, the
// last status that was built is better than none.
type statusCache struct {
	mu   sync.Mutex // held while building, so concurrent requests share one build
	page status.Page
	at   time.Time
	ok   bool
}

// ownerStatus is the public status. stale means it couldn't be built just now and this is the
// last one that was (page.UpdatedAt says when). An error means there is nothing to show at all.
func (s *Server) ownerStatus(ctx context.Context) (page status.Page, stale bool, err error) {
	c := &s.status
	c.mu.Lock()
	defer c.mu.Unlock()
	now := s.Now()
	if c.ok && s.Config.StatusCache > 0 && now.Sub(c.at) < s.Config.StatusCache {
		return c.page, false, nil
	}
	// Not tied to this request alone: others are waiting on the same build.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	page, err = status.Build(ctx, s.Pool, s.OwnerTenant, now)
	if err != nil {
		if c.ok {
			s.Log.Warn("status: serving the last one built", "from", c.page.UpdatedAt, "err", err)
			return c.page, true, nil
		}
		return page, false, err
	}
	c.page, c.at, c.ok = page, now, true
	return page, false, nil
}
