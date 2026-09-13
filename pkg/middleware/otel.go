package middleware

import (
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "github.com/guilhermebr/gox/pkg/middleware"

	attrMethod = "http.request.method"
	attrRoute  = "http.route"
	attrStatus = "http.response.status_code"
)

// Tracing starts a server span per request with otelhttp, continues
// incoming trace context, and renames the span after the mux pattern that
// matched ("GET /items/{id}") so traces group by route, not by URL.
func Tracing(tp trace.TracerProvider, prop propagation.TextMapPropagator) Middleware {
	otel := otelhttp.NewMiddleware("http.server",
		otelhttp.WithTracerProvider(tp),
		otelhttp.WithPropagators(prop),
		// gox records the semconv metrics itself (Metrics), keyed by the mux
		// pattern; otelhttp's own instruments are switched off.
		otelhttp.WithMeterProvider(noop.NewMeterProvider()),
	)
	enrich := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			span := trace.SpanFromContext(r.Context())
			if !span.IsRecording() {
				return
			}
			span.SetName(routeOf(r))
			span.SetAttributes(attribute.String(attrRoute, pathPattern(r)))
		})
	}
	return Chain(otel, enrich)
}

// Metrics records http.server.request.duration (seconds) per method, route
// and status, following the OpenTelemetry HTTP semantic conventions.
func Metrics(mp metric.MeterProvider) (Middleware, error) {
	meter := mp.Meter(instrumentationName)
	duration, err := meter.Float64Histogram("http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of HTTP server requests."),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10),
	)
	if err != nil {
		return nil, fmt.Errorf("middleware: metrics: %w", err)
	}
	active, err := meter.Int64UpDownCounter("http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("Number of in-flight HTTP server requests."))
	if err != nil {
		return nil, fmt.Errorf("middleware: metrics: %w", err)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			active.Add(ctx, 1)
			rec := newRecorder(w)
			began := time.Now()
			next.ServeHTTP(rec, r)
			active.Add(ctx, -1)
			duration.Record(ctx, time.Since(began).Seconds(), metric.WithAttributes(
				attribute.String(attrMethod, r.Method),
				attribute.String(attrRoute, pathPattern(r)),
				attribute.Int(attrStatus, rec.status),
			))
		})
	}, nil
}
