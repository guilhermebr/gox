-- First migration: replace with your schema. Keep migrations forward-only
-- in production; the .down.sql exists for local resets.
CREATE TABLE IF NOT EXISTS app_info (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
