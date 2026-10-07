-- +goose Up

-- More things a visitor can do that are worth knowing about: taking the CV, going on to GitHub or
-- LinkedIn, starting an email, reading a story to its end. Still one of each per page view, and
-- still nothing about who.
ALTER TABLE analytics_events DROP CONSTRAINT analytics_events_name_check;
ALTER TABLE analytics_events ADD CONSTRAINT analytics_events_name_check CHECK (name IN (
  'demo_ready', 'demo_open', 'intro_skip',
  'cv_download', 'outbound_github', 'outbound_linkedin', 'contact_email', 'read_to_end'));

-- +goose Down
DELETE FROM analytics_events WHERE name NOT IN ('demo_ready', 'demo_open', 'intro_skip');
ALTER TABLE analytics_events DROP CONSTRAINT analytics_events_name_check;
ALTER TABLE analytics_events ADD CONSTRAINT analytics_events_name_check CHECK (name IN ('demo_ready', 'demo_open', 'intro_skip'));
