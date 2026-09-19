// Package jobs runs background jobs on Postgres with River: transactional
// inserts, retries with backoff, cron schedules, and a worker that starts
// and drains with the app.
package jobs
