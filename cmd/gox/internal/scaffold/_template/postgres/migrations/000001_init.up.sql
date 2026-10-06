-- The first migration. Edit it only before it has run anywhere; later changes
-- go in 000002_<change>.up.sql and up. gox runs only up migrations; the
-- .down.sql is for resetting a local database by hand.
CREATE TABLE IF NOT EXISTS app_info (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
