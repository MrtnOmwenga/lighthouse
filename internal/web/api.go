package web

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MrtnOmwenga/lighthouse/internal/auth"
	"github.com/MrtnOmwenga/lighthouse/internal/monitor"
	"github.com/MrtnOmwenga/lighthouse/internal/status"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
)

// pathID reads a UUID path parameter. Anything else is simply not found.
func pathID(r *http.Request) (string, error) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		return "", errNotFound
	}
	return id, nil
}

func author(id auth.Identity) string {
	if id.Login != "" {
		return id.Login
	}
	return "Sandbox visitor"
}

// Monitors.

func (s *Server) listMonitors(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	var list []store.Monitor
	if err := s.inTenant(r, id, func(tx pgx.Tx) (err error) { list, err = store.ListMonitors(r.Context(), tx, false); return }); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, list)
}

func (s *Server) createMonitor(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	var in store.MonitorInput
	if err := readJSON(w, r, &in); err != nil {
		return err
	}
	in, err := normalize(in, id)
	if err != nil {
		return err
	}
	var m store.Monitor
	err = s.inTenant(r, id, func(tx pgx.Tx) error {
		if !id.Owner() {
			var n int
			if err := tx.QueryRow(r.Context(), "SELECT count(*) FROM monitors").Scan(&n); err != nil {
				return err
			}
			if n >= maxSandboxMonitors {
				return invalid("The sandbox holds up to 10 monitors.")
			}
		}
		m, err = store.CreateMonitor(r.Context(), tx, id.TenantID, in)
		return err
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, m)
}

func (s *Server) getMonitor(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	mid, err := pathID(r)
	if err != nil {
		return err
	}
	var m store.Monitor
	if err := s.inTenant(r, id, func(tx pgx.Tx) (err error) { m, err = store.GetMonitor(r.Context(), tx, mid); return }); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, m)
}

func (s *Server) updateMonitor(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	mid, err := pathID(r)
	if err != nil {
		return err
	}
	var in store.MonitorInput
	if err := readJSON(w, r, &in); err != nil {
		return err
	}
	if in, err = normalize(in, id); err != nil {
		return err
	}
	var m store.Monitor
	if err := s.inTenant(r, id, func(tx pgx.Tx) (err error) { m, err = store.UpdateMonitor(r.Context(), tx, mid, in); return }); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, m)
}

// onRequestTimeout bounds a check made while someone waits for the answer; a probe may take up to
// 30 s, longer than the server lets a response take.
const onRequestTimeout = 25 * time.Second

// checkMonitor checks a monitor now and returns the monitor and that check: the console calls it
// after saving, and from "Check now".
func (s *Server) checkMonitor(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	mid, err := pathID(r)
	if err != nil {
		return err
	}
	if s.Checks == nil {
		return errNotFound
	}
	ctx, cancel := context.WithTimeout(r.Context(), onRequestTimeout)
	defer cancel()
	check, err := s.Checks.CheckNow(ctx, id.TenantID, mid)
	if errors.Is(err, monitor.ErrPaused) {
		return problem{status: http.StatusConflict, title: "Paused", detail: "Resume the monitor to check it."}
	}
	if err != nil {
		return err
	}
	var m store.Monitor
	if err := s.inTenant(r, id, func(tx pgx.Tx) (err error) { m, err = store.GetMonitor(r.Context(), tx, mid); return }); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{"monitor": m, "check": check})
}

// testMonitor tries a monitor's settings without saving them or recording anything. The settings
// pass the same validation as a saved monitor, so a sandbox can only try simulated ones.
func (s *Server) testMonitor(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	var in store.MonitorInput
	if err := readJSON(w, r, &in); err != nil {
		return err
	}
	if in.Name == "" {
		in.Name = "test" // not part of what is being tried
	}
	in, err := normalize(in, id)
	if err != nil {
		return err
	}
	if s.Checks == nil {
		return errNotFound
	}
	ctx, cancel := context.WithTimeout(r.Context(), onRequestTimeout)
	defer cancel()
	res := s.Checks.Try(ctx, in)
	out := map[string]any{"ok": res.OK, "latencyMs": res.Latency.Milliseconds()}
	if res.StatusCode != 0 {
		out["statusCode"] = res.StatusCode
	}
	if res.Failure != "" {
		out["failure"] = res.Failure
	}
	return writeJSON(w, http.StatusOK, out)
}

// overview is everything the monitors screen shows, in one answer: the monitors and their figures.
func (s *Server) overview(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	var list []store.Monitor
	if err := s.inTenant(r, id, func(tx pgx.Tx) (err error) { list, err = store.ListMonitors(r.Context(), tx, false); return }); err != nil {
		return err
	}
	page, err := status.BuildAll(r.Context(), s.Pool, id.TenantID, s.Now())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{"monitors": list, "stats": page.Monitors})
}

func (s *Server) deleteMonitor(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	mid, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.inTenant(r, id, func(tx pgx.Tx) error { return store.DeleteMonitor(r.Context(), tx, mid) }); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listChecks(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	mid, err := pathID(r)
	if err != nil {
		return err
	}
	var before int64
	if b := r.URL.Query().Get("before"); b != "" {
		if before, err = strconv.ParseInt(b, 10, 64); err != nil || before < 0 {
			return invalid("before: a check id.")
		}
	}
	var checks []store.Check
	err = s.inTenant(r, id, func(tx pgx.Tx) error {
		if _, err := store.GetMonitor(r.Context(), tx, mid); err != nil {
			return err
		}
		checks, err = store.RecentChecks(r.Context(), tx, mid, before, 100)
		return err
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, checks)
}

// setMode switches a simulated monitor between up, slow, flaky and down: the sandbox's "break it"
// button. The next check runs straight away.
func (s *Server) setMode(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	mid, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if err := readJSON(w, r, &in); err != nil {
		return err
	}
	if !oneOf(in.Mode, "up", "slow", "flaky", "down") {
		return invalid("mode: up, slow, flaky or down.")
	}
	var m store.Monitor
	if err := s.inTenant(r, id, func(tx pgx.Tx) (err error) { m, err = store.SetSimulatedMode(r.Context(), tx, mid, in.Mode); return }); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, m)
}

func (s *Server) myStatus(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	page, err := status.Build(r.Context(), s.Pool, id.TenantID, s.Now())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, page)
}

// Incidents.

type incidentPage struct {
	Incidents []store.Incident `json:"incidents"`
	Next      string           `json:"next,omitempty"` // pass as ?cursor= for the next page
}

func encodeCursor(i store.Incident) string {
	return base64.RawURLEncoding.EncodeToString([]byte(i.StartedAt.Format(time.RFC3339Nano) + "|" + i.ID))
}

func decodeCursor(raw string) (*store.IncidentCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, invalid("cursor: not a cursor from this API.")
	}
	at, id, ok := strings.Cut(string(b), "|")
	t, err := time.Parse(time.RFC3339Nano, at)
	if _, uerr := uuid.Parse(id); !ok || err != nil || uerr != nil {
		return nil, invalid("cursor: not a cursor from this API.")
	}
	return &store.IncidentCursor{StartedAt: t, ID: id}, nil
}

func (s *Server) listIncidents(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	const pageSize = 25
	f := store.IncidentFilter{Limit: pageSize + 1}
	if c := r.URL.Query().Get("cursor"); c != "" {
		cur, err := decodeCursor(c)
		if err != nil {
			return err
		}
		f.Before = cur
	}
	var list []store.Incident
	if err := s.inTenant(r, id, func(tx pgx.Tx) (err error) { list, err = store.ListIncidents(r.Context(), tx, f); return }); err != nil {
		return err
	}
	out := incidentPage{Incidents: list}
	if len(list) > pageSize {
		out.Incidents = list[:pageSize]
		out.Next = encodeCursor(list[pageSize-1])
	}
	if out.Incidents == nil {
		out.Incidents = []store.Incident{}
	}
	return writeJSON(w, http.StatusOK, out)
}

type incidentDetail struct {
	store.Incident
	Events []store.Event `json:"events"`
}

func (s *Server) getIncident(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	iid, err := pathID(r)
	if err != nil {
		return err
	}
	var d incidentDetail
	err = s.inTenant(r, id, func(tx pgx.Tx) (err error) {
		if d.Incident, err = store.GetIncident(r.Context(), tx, iid); err != nil {
			return err
		}
		d.Events, err = store.Events(r.Context(), tx, iid, false)
		return err
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, d)
}

func validMessage(m string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(m))
	return n >= 1 && n <= 2000
}

func (s *Server) createIncident(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	var in struct {
		Title     string  `json:"title"`
		Severity  string  `json:"severity"`
		Public    bool    `json:"public"`
		MonitorID *string `json:"monitorId"`
		Message   string  `json:"message"`
	}
	if err := readJSON(w, r, &in); err != nil {
		return err
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Severity == "" {
		in.Severity = "medium"
	}
	switch {
	case in.Title == "" || utf8.RuneCountInString(in.Title) > 200:
		return invalid("title: 1 to 200 characters.")
	case !oneOf(in.Severity, "low", "medium", "high"):
		return invalid("severity: low, medium or high.")
	case !validMessage(in.Message):
		return invalid("message: what's happening, 1 to 2000 characters.")
	}
	if in.MonitorID != nil {
		if _, err := uuid.Parse(*in.MonitorID); err != nil {
			return invalid("monitorId: a monitor's id.")
		}
	}
	var d incidentDetail
	err := s.inTenant(r, id, func(tx pgx.Tx) (err error) {
		if in.MonitorID != nil {
			if _, err := store.GetMonitor(r.Context(), tx, *in.MonitorID); err != nil {
				return invalid("monitorId: no such monitor.")
			}
		}
		now := s.Now()
		if d.Incident, err = store.CreateIncident(r.Context(), tx, id.TenantID, store.Incident{
			MonitorID: in.MonitorID, Title: in.Title, Severity: in.Severity, Public: in.Public, StartedAt: now,
		}); err != nil {
			return err
		}
		e, err := store.AddEvent(r.Context(), tx, id.TenantID, store.Event{
			IncidentID: d.ID, At: now, Kind: "opened", Message: strings.TrimSpace(in.Message), Public: in.Public, Author: author(id),
		})
		d.Events = []store.Event{e}
		return err
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, d)
}

// updateIncident changes status and/or severity; each change is recorded on the timeline.
func (s *Server) updateIncident(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	iid, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct {
		Status   *string `json:"status"`
		Severity *string `json:"severity"`
	}
	if err := readJSON(w, r, &in); err != nil {
		return err
	}
	if in.Status != nil && !oneOf(*in.Status, "open", "in_progress", "resolved") {
		return invalid("status: open, in_progress or resolved.")
	}
	if in.Severity != nil && !oneOf(*in.Severity, "low", "medium", "high") {
		return invalid("severity: low, medium or high.")
	}
	var inc store.Incident
	err = s.inTenant(r, id, func(tx pgx.Tx) (err error) {
		if inc, err = store.GetIncident(r.Context(), tx, iid); err != nil {
			return err
		}
		now := s.Now()
		event := func(kind, msg string) error {
			_, err := store.AddEvent(r.Context(), tx, id.TenantID, store.Event{
				IncidentID: iid, At: now, Kind: kind, Message: msg, Public: inc.Public, Author: author(id),
			})
			return err
		}
		if in.Status != nil && *in.Status != inc.Status {
			if inc, err = store.SetIncidentStatus(r.Context(), tx, iid, *in.Status, now); err != nil {
				return err
			}
			if err := event("status", "Status changed to "+strings.ReplaceAll(*in.Status, "_", " ")+"."); err != nil {
				return err
			}
		}
		if in.Severity != nil && *in.Severity != inc.Severity {
			if inc, err = store.SetIncidentSeverity(r.Context(), tx, iid, *in.Severity); err != nil {
				return err
			}
			return event("severity", "Severity changed to "+*in.Severity+".")
		}
		return nil
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, inc)
}

// addComment posts an update on the timeline. Public updates appear on the status page; the
// others are internal notes.
func (s *Server) addComment(w http.ResponseWriter, r *http.Request, id auth.Identity) error {
	iid, err := pathID(r)
	if err != nil {
		return err
	}
	var in struct {
		Message string `json:"message"`
		Public  bool   `json:"public"`
	}
	if err := readJSON(w, r, &in); err != nil {
		return err
	}
	if !validMessage(in.Message) {
		return invalid("message: 1 to 2000 characters.")
	}
	var e store.Event
	err = s.inTenant(r, id, func(tx pgx.Tx) error {
		inc, err := store.GetIncident(r.Context(), tx, iid)
		if err != nil {
			return err
		}
		e, err = store.AddEvent(r.Context(), tx, id.TenantID, store.Event{
			IncidentID: iid, At: s.Now(), Kind: "comment", Message: strings.TrimSpace(in.Message),
			Public: in.Public && inc.Public, Author: author(id), // nothing on a private incident is public
		})
		return err
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, e)
}
