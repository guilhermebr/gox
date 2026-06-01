package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// QueryMiddleware provides query instrumentation and monitoring
type QueryMiddleware struct {
	pool *DatabasePool
}

// NewQueryMiddleware creates a new query instrumentation middleware
func NewQueryMiddleware(pool *DatabasePool) *QueryMiddleware {
	return &QueryMiddleware{
		pool: pool,
	}
}

// InstrumentedConn wraps a database connection with instrumentation
type InstrumentedConn struct {
	pgx.Conn
	middleware *QueryMiddleware
}

// InstrumentedTx wraps a database transaction with instrumentation
type InstrumentedTx struct {
	pgx.Tx
	middleware *QueryMiddleware
	startTime  time.Time
}

// Query executes a query with instrumentation
func (c *InstrumentedConn) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	start := time.Now()
	rows, err := c.Conn.Query(ctx, sql, args...)
	duration := time.Since(start)

	operation, table := parseSQL(sql)
	c.middleware.pool.RecordQuery(operation, table, duration, err)

	return rows, err
}

// QueryRow executes a single-row query with instrumentation.
// Note: QueryRow errors are not captured until Scan() is called on the returned Row.
// Only query execution time is recorded here; scan errors must be handled separately.
func (c *InstrumentedConn) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	start := time.Now()
	row := c.Conn.QueryRow(ctx, sql, args...)
	duration := time.Since(start)

	operation, table := parseSQL(sql)
	c.middleware.pool.RecordQuery(operation, table, duration, nil)

	return row
}

// Exec executes a query with instrumentation
func (c *InstrumentedConn) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	start := time.Now()
	result, err := c.Conn.Exec(ctx, sql, args...)
	duration := time.Since(start)

	operation, table := parseSQL(sql)
	c.middleware.pool.RecordQuery(operation, table, duration, err)

	return result, err
}

// Begin starts a transaction with instrumentation
func (c *InstrumentedConn) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := c.Conn.Begin(ctx)
	if err != nil {
		return nil, err
	}

	return &InstrumentedTx{
		Tx:         tx,
		middleware: c.middleware,
		startTime:  time.Now(),
	}, nil
}

// Query executes a query within a transaction with instrumentation
func (tx *InstrumentedTx) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	start := time.Now()
	rows, err := tx.Tx.Query(ctx, sql, args...)
	duration := time.Since(start)

	operation, table := parseSQL(sql)
	tx.middleware.pool.RecordQuery(operation, table, duration, err)

	return rows, err
}

// QueryRow executes a single-row query within a transaction with instrumentation.
// Note: QueryRow errors are not captured until Scan() is called on the returned Row.
// Only query execution time is recorded here; scan errors must be handled separately.
func (tx *InstrumentedTx) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	start := time.Now()
	row := tx.Tx.QueryRow(ctx, sql, args...)
	duration := time.Since(start)

	operation, table := parseSQL(sql)
	tx.middleware.pool.RecordQuery(operation, table, duration, nil)

	return row
}

// Exec executes a query within a transaction with instrumentation
func (tx *InstrumentedTx) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	start := time.Now()
	result, err := tx.Tx.Exec(ctx, sql, args...)
	duration := time.Since(start)

	operation, table := parseSQL(sql)
	tx.middleware.pool.RecordQuery(operation, table, duration, err)

	return result, err
}

// Commit commits the transaction and records transaction duration
func (tx *InstrumentedTx) Commit(ctx context.Context) error {
	defer func() {
		duration := time.Since(tx.startTime)
		tx.middleware.pool.RecordTransaction(duration)
	}()

	return tx.Tx.Commit(ctx)
}

// Rollback rolls back the transaction and records transaction duration
func (tx *InstrumentedTx) Rollback(ctx context.Context) error {
	defer func() {
		duration := time.Since(tx.startTime)
		tx.middleware.pool.RecordTransaction(duration)
	}()

	return tx.Tx.Rollback(ctx)
}

// QueryExecutor provides a unified interface for instrumented query execution
type QueryExecutor struct {
	pool       *DatabasePool
	middleware *QueryMiddleware
}

// NewQueryExecutor creates a new query executor with instrumentation
func NewQueryExecutor(pool *DatabasePool) *QueryExecutor {
	return &QueryExecutor{
		pool:       pool,
		middleware: NewQueryMiddleware(pool),
	}
}

// QueryWithInstrumentation executes a query with automatic instrumentation
func (qe *QueryExecutor) QueryWithInstrumentation(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	start := time.Now()
	rows, err := qe.pool.Query(ctx, sql, args...)
	duration := time.Since(start)

	operation, table := parseSQL(sql)
	qe.pool.RecordQuery(operation, table, duration, err)

	return rows, err
}

// QueryRowWithInstrumentation executes a single-row query with automatic instrumentation.
// Note: QueryRow errors are not captured until Scan() is called on the returned Row.
// Only query execution time is recorded here; scan errors must be handled separately.
func (qe *QueryExecutor) QueryRowWithInstrumentation(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	start := time.Now()
	row := qe.pool.QueryRow(ctx, sql, args...)
	duration := time.Since(start)

	operation, table := parseSQL(sql)
	qe.pool.RecordQuery(operation, table, duration, nil)

	return row
}

// ExecWithInstrumentation executes a query with automatic instrumentation
func (qe *QueryExecutor) ExecWithInstrumentation(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	start := time.Now()
	result, err := qe.pool.Exec(ctx, sql, args...)
	duration := time.Since(start)

	operation, table := parseSQL(sql)
	qe.pool.RecordQuery(operation, table, duration, err)

	return result, err
}

// BeginTxWithInstrumentation starts a transaction with instrumentation
func (qe *QueryExecutor) BeginTxWithInstrumentation(ctx context.Context) (*InstrumentedTx, error) {
	tx, err := qe.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}

	return &InstrumentedTx{
		Tx:         tx,
		middleware: qe.middleware,
		startTime:  time.Now(),
	}, nil
}

// WithTx executes a function within a transaction with automatic rollback on error
func (qe *QueryExecutor) WithTx(ctx context.Context, fn func(*InstrumentedTx) error) error {
	tx, err := qe.BeginTxWithInstrumentation(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback(ctx)
			panic(p)
		}
	}()

	err = fn(tx)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			// Log rollback error but return original error
			return err
		}
		return err
	}

	return tx.Commit(ctx)
}
