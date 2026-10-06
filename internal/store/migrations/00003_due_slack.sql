-- +goose Up

-- A schedule driven from outside (config.Schedule) calls in at fixed times, and each check moves
-- a monitor's next one a full interval on from the moment it ran. When the interval equals the
-- calling period, the next call lands a few seconds too early and the monitor waits a whole extra
-- period. p_slack lets a caller treat a monitor as due slightly ahead of time, never by more than
-- a tenth of its interval. The default keeps the old meaning, so the previous release keeps
-- working while this migration is live.
DROP FUNCTION lighthouse_due_monitors(integer);

-- +goose StatementBegin
CREATE FUNCTION lighthouse_due_monitors(p_limit integer, p_slack interval DEFAULT '0')
  RETURNS TABLE (id uuid, tenant_id uuid) LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$
    SELECT id, tenant_id FROM monitors
    WHERE NOT paused AND next_check_at <= now() + least(p_slack, make_interval(secs => interval_seconds / 10.0))
    ORDER BY next_check_at LIMIT p_limit
  $$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION lighthouse_due_monitors(integer, interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lighthouse_due_monitors(integer, interval) TO lighthouse_app;

-- +goose Down
DROP FUNCTION lighthouse_due_monitors(integer, interval);

-- +goose StatementBegin
CREATE FUNCTION lighthouse_due_monitors(p_limit integer)
  RETURNS TABLE (id uuid, tenant_id uuid) LANGUAGE sql STABLE SECURITY DEFINER SET search_path = public
  AS $$ SELECT id, tenant_id FROM monitors WHERE NOT paused AND next_check_at <= now() ORDER BY next_check_at LIMIT p_limit $$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION lighthouse_due_monitors(integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lighthouse_due_monitors(integer) TO lighthouse_app;
