package postgres

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/guilhermebr/gox"
)

const instrumentationName = "github.com/guilhermebr/gox/postgres"

// tracer creates a client span per query with the statement as db.query.text.
// Arguments are never recorded.
type tracer struct {
	tracer trace.Tracer
	log    *slog.Logger
}

func newTracer(a *gox.App) *tracer {
	return &tracer{tracer: otel.GetTracerProvider().Tracer(instrumentationName), log: a.Log()}
}

func (t *tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, _ = t.tracer.Start(ctx, "postgres.query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.query.text", data.SQL),
		))
	return ctx
}

func (t *tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	} else {
		span.SetAttributes(attribute.Int64("db.response.returned_rows", data.CommandTag.RowsAffected()))
	}
	span.End()
}

var _ pgx.QueryTracer = (*tracer)(nil)

// registerPoolMetrics exports pool stats as OpenTelemetry gauges and returns
// a function that unregisters them.
func registerPoolMetrics(pool *pgxpool.Pool) func() {
	meter := otel.GetMeterProvider().Meter(instrumentationName)
	conns, err1 := meter.Int64ObservableGauge("db.client.connection.count",
		metric.WithDescription("Connections in the pool by state."))
	maxConns, err2 := meter.Int64ObservableGauge("db.client.connection.max",
		metric.WithDescription("Maximum connections the pool may open."))
	waits, err3 := meter.Int64ObservableCounter("db.client.connection.wait_count",
		metric.WithDescription("Times a caller waited for a connection."))
	if err1 != nil || err2 != nil || err3 != nil {
		return func() {}
	}
	reg, err := meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := pool.Stat()
		o.ObserveInt64(conns, int64(s.IdleConns()), metric.WithAttributes(attribute.String("state", "idle")))
		o.ObserveInt64(conns, int64(s.AcquiredConns()), metric.WithAttributes(attribute.String("state", "used")))
		o.ObserveInt64(maxConns, int64(s.MaxConns()))
		o.ObserveInt64(waits, s.EmptyAcquireCount())
		return nil
	}, conns, maxConns, waits)
	if err != nil {
		return func() {}
	}
	return func() { _ = reg.Unregister() }
}
