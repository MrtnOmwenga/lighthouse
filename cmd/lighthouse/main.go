// Command lighthouse runs the uptime monitor.
//
//	lighthouse serve        run the web server, the scheduler and housekeeping (the default; with
//	                        SCHEDULE=external, POST /internal/tick drives the checks instead)
//	lighthouse migrate      apply database migrations (needs MIGRATE_DATABASE_URL, the owner role)
//	lighthouse healthcheck  exit 0 if the local server answers /healthz (for container health checks)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MrtnOmwenga/lighthouse/internal/alert"
	"github.com/MrtnOmwenga/lighthouse/internal/auth"
	"github.com/MrtnOmwenga/lighthouse/internal/config"
	"github.com/MrtnOmwenga/lighthouse/internal/metrics"
	"github.com/MrtnOmwenga/lighthouse/internal/monitor"
	"github.com/MrtnOmwenga/lighthouse/internal/oidc"
	"github.com/MrtnOmwenga/lighthouse/internal/site"
	"github.com/MrtnOmwenga/lighthouse/internal/store"
	"github.com/MrtnOmwenga/lighthouse/internal/web"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "serve":
		err = serve(ctx, log)
	case "migrate":
		err = migrate(ctx)
	case "healthcheck":
		err = healthcheck()
	default:
		err = fmt.Errorf("unknown command %q: use serve, migrate or healthcheck", cmd)
	}
	if err != nil {
		log.Error(cmd+" failed", "err", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()
	owner, err := store.OwnerTenant(ctx, pool, cfg.OwnerName)
	if err != nil {
		return fmt.Errorf("owner tenant: %w", err)
	}

	content, err := site.Load(cfg.SiteDir, cfg.Domain())
	if err != nil {
		return err
	}

	scheduler := &monitor.Scheduler{Pool: pool, Prober: monitor.NewProber(), Workers: cfg.CheckWorkers, Log: log,
		Confirm: cfg.ConfirmAfter, Warm: cfg.WarmAbove}
	if len(cfg.AlertTo) > 0 {
		notifier := &alert.Notifier{
			Mailer: &alert.Mailer{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
				From: cfg.AlertFrom, To: cfg.AlertTo},
			Tenant: owner, PublicURL: cfg.PublicURL, Log: log,
		}
		scheduler.Notify = notifier.Notify
		log.Info("email alerts on", "to", len(cfg.AlertTo))
	}
	keepChecks := time.Duration(cfg.RetentionDays) * 24 * time.Hour
	done := make(chan struct{}, 2)
	var tick web.Ticker
	var registry *metrics.Registry
	if cfg.ExternalSchedule() {
		// Something else keeps time (Cloud Scheduler calling /internal/tick): an idle instance may
		// get no CPU, so a clock of our own would stall.
		scheduler.Slack = cfg.TickSlack
		registry = metrics.NewRegistry()
		report := reporter(pool, log, owner, registry, &metrics.Pusher{URL: cfg.MetricsPushURL, User: cfg.MetricsPushUser, Token: cfg.MetricsPushToken})
		tick = externalTicker(pool, log, scheduler, keepChecks, cfg.SandboxTTL, report)
		done <- struct{}{}
		done <- struct{}{}
		log.Info("checks scheduled externally", "audience", cfg.TickAudience, "caller", cfg.TickCaller)
	} else {
		go func() { scheduler.Run(ctx); done <- struct{}{} }()
		go func() { monitor.Prune(ctx, pool, log, keepChecks, cfg.SandboxTTL); done <- struct{}{} }()
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler(cfg, pool, log, owner, content, tick, scheduler, registry),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 << 10,
	}
	errs := make(chan error, 1)
	go func() { errs <- srv.ListenAndServe() }()
	log.Info("lighthouse listening", "addr", cfg.Addr, "env", cfg.Env, "public_url", cfg.PublicURL, "dev_login", cfg.DevLogin)

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdown)
	<-done
	<-done
	return err
}

func handler(cfg config.Config, pool *pgxpool.Pool, log *slog.Logger, owner string, content *site.Site, tick web.Ticker, scheduler *monitor.Scheduler, registry *metrics.Registry) http.Handler {
	srv := web.New(cfg, pool, auth.New(pool, cfg), log, owner)
	srv.Site = content
	srv.Checks = scheduler
	srv.Metrics = registry
	if tick != nil {
		srv.Tick = tick
		srv.TickVerifier = &oidc.Verifier{Audience: cfg.TickAudience, Email: cfg.TickCaller}
		srv.Drive = scheduler.RunTenant
	}
	return srv.Handler()
}

// externalTicker runs the checks that are due on each call, and housekeeping at most hourly per
// instance (pruning twice is harmless; it's only wasted work).
func externalTicker(pool *pgxpool.Pool, log *slog.Logger, s *monitor.Scheduler, keepChecks, keepSandboxes time.Duration,
	report func(ctx context.Context, checks int, took time.Duration)) web.Ticker {
	var mu sync.Mutex
	var lastPrune time.Time
	return func(ctx context.Context) (int, error) {
		started := time.Now()
		n := s.RunOnce(ctx)
		report(ctx, n, time.Since(started))
		mu.Lock()
		due := time.Since(lastPrune) >= time.Hour
		if due {
			lastPrune = time.Now()
		}
		mu.Unlock()
		if due {
			monitor.PruneOnce(ctx, pool, log, keepChecks, keepSandboxes)
		}
		return n, ctx.Err()
	}
}

// reporter sends a round's figures outside when it ends: how the round went, each of the owner's
// monitors, open incidents, and the requests answered since the last round. When these stop
// arriving, the service receiving them knows Lighthouse itself is down. A failed push is logged
// and never fails the round.
func reporter(pool *pgxpool.Pool, log *slog.Logger, owner string, registry *metrics.Registry, pusher *metrics.Pusher) func(context.Context, int, time.Duration) {
	return func(ctx context.Context, checks int, took time.Duration) {
		samples := []metrics.Sample{{Name: "lighthouse_tick", Fields: map[string]float64{"checks": float64(checks), "duration_seconds": took.Seconds()}}}
		err := store.WithTenant(ctx, pool, owner, func(tx pgx.Tx) error {
			monitors, err := store.ListMonitors(ctx, tx, false)
			if err != nil {
				return err
			}
			for _, m := range monitors {
				up := 0.0
				if m.Health == "up" {
					up = 1
				}
				samples = append(samples, metrics.Sample{Name: "lighthouse_monitor", Labels: map[string]string{"monitor": m.Slug}, Fields: map[string]float64{"up": up}})
			}
			open, err := store.OpenIncidents(ctx, tx)
			samples = append(samples, metrics.Sample{Name: "lighthouse_incidents", Fields: map[string]float64{"open": float64(open)}})
			return err
		})
		if err != nil && ctx.Err() == nil {
			log.Error("metrics: reading the round's figures", "err", err)
		}
		samples = append(samples, registry.Drain()...)
		if err := pusher.Push(ctx, samples, time.Now()); err != nil && ctx.Err() == nil {
			log.Error("metrics: push failed", "err", err)
		}
	}
}

func migrate(ctx context.Context) error {
	owner := os.Getenv("MIGRATE_DATABASE_URL")
	if owner == "" {
		return errors.New("MIGRATE_DATABASE_URL (the database owner) is required")
	}
	if err := store.Migrate(ctx, owner, os.Getenv("DATABASE_URL"), os.Getenv("APP_DB_PASSWORD")); err != nil {
		return err
	}
	fmt.Println("migrations applied")
	return nil
}

func healthcheck() error {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz answered %d", resp.StatusCode)
	}
	return nil
}
