-- +goose Up

-- Sessions get an id that can be shown and acted on (the token hash never leaves the server), and
-- a note of what kind of device started them, so the owner can tell his sessions apart.
ALTER TABLE sessions ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE sessions ADD COLUMN device text NOT NULL DEFAULT '' CHECK (length(device) <= 40);
CREATE UNIQUE INDEX sessions_id ON sessions (id);

-- A record of sign-ins, in the owner's tenant: who got in, who was turned away, and when sessions
-- were ended. No network addresses are kept. The application can add to it and read it, nothing
-- else.
CREATE TABLE auth_events (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
  at timestamptz NOT NULL DEFAULT now(),
  kind text NOT NULL CHECK (kind IN ('signed_in', 'refused', 'signed_out', 'sessions_ended')),
  login text NOT NULL DEFAULT '' CHECK (length(login) <= 100),
  github_id bigint,
  detail text NOT NULL DEFAULT '' CHECK (length(detail) <= 200)
);
CREATE INDEX auth_events_recent ON auth_events (tenant_id, at DESC);
ALTER TABLE auth_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE auth_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON auth_events
  USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
GRANT SELECT, INSERT ON auth_events TO lighthouse_app;

-- Counters for limits that must survive a restart and be shared between instances (new sandboxes
-- per visitor). Keys are keyed hashes, never addresses. The application reaches this table only
-- through the function below.
CREATE TABLE rate_limits (
  key text NOT NULL,
  window_start timestamptz NOT NULL,
  count integer NOT NULL,
  PRIMARY KEY (key, window_start)
);

-- +goose StatementBegin
-- Counts one more use of p_key in the current window and says whether it is within p_limit.
CREATE FUNCTION lighthouse_rate_hit(p_key text, p_window interval, p_limit integer)
  RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = public
  AS $$
  DECLARE
    w timestamptz := to_timestamp(floor(extract(epoch FROM now()) / extract(epoch FROM p_window)) * extract(epoch FROM p_window));
    n integer;
  BEGIN
    INSERT INTO rate_limits AS r (key, window_start, count) VALUES (p_key, w, 1)
      ON CONFLICT (key, window_start) DO UPDATE SET count = r.count + 1
      RETURNING count INTO n;
    RETURN n <= p_limit;
  END $$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Housekeeping for this migration's tables: old sign-in records and finished windows.
CREATE FUNCTION lighthouse_prune_security(p_keep interval)
  RETURNS bigint LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = public
  AS $$
    WITH e AS (DELETE FROM auth_events WHERE at < now() - p_keep RETURNING 1),
         r AS (DELETE FROM rate_limits WHERE window_start < now() - interval '1 day' RETURNING 1)
    SELECT (SELECT count(*) FROM e) + (SELECT count(*) FROM r)
  $$;
-- +goose StatementEnd

REVOKE ALL ON FUNCTION lighthouse_rate_hit(text, interval, integer), lighthouse_prune_security(interval) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION lighthouse_rate_hit(text, interval, integer), lighthouse_prune_security(interval) TO lighthouse_app;

-- +goose Down
DROP FUNCTION lighthouse_prune_security(interval);
DROP FUNCTION lighthouse_rate_hit(text, interval, integer);
DROP TABLE rate_limits, auth_events;
DROP INDEX sessions_id;
ALTER TABLE sessions DROP COLUMN device, DROP COLUMN id;
