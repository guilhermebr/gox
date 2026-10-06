// Package postgres gives a gox service one pgx connection pool. Pass
// Enable(WithMigrations(migrations.FS)) to gox.New: it reads the
// <PREFIX>_POSTGRES_* variables, pings the database at Run, applies the
// embedded migrations before the app reports ready, and makes /readyz ping
// it. From(a) returns the *pgxpool.Pool. Tx and TxWith run a function
// in a transaction; Migrate applies the same migrations from a deploy step or
// a test. Queries are traced and pool stats exported as metrics.
package postgres
