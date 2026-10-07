-- +goose Up

-- A check that woke a sleeping service: it counts towards uptime (the service answered), and is
-- left out of response-time figures, which would otherwise report the time it takes to wake up.
ALTER TABLE checks ADD COLUMN warmup boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE checks DROP COLUMN warmup;
