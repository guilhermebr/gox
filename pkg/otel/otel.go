// Package otel sets up OpenTelemetry for a service: a tracer provider that
// exports over OTLP when enabled, a meter provider that always feeds a
// Prometheus handler for the admin server (and OTLP when enabled), W3C
// propagation, and a hook that stamps trace and span ids on log records.
//
// Export is on when OTEL_ENABLED is "true", off when "false", and in "auto"
// mode on in production or whenever an endpoint is configured.
package otel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/guilhermebr/gox/pkg/log"
)

// Config is what Setup needs; the root fills it from config.Base.
type Config struct {
	ServiceName string
	Version     string
	Environment string
	Enabled     string // auto | true | false
	Endpoint    string // host:port of the OTLP collector
	Protocol    string // grpc | http
	Insecure    bool   // plaintext to the collector
}

// Providers is what Setup returns. Tracer and Meter are also installed as
// the OpenTelemetry globals so third-party instrumentation finds them.
type Providers struct {
	Tracer     trace.TracerProvider
	Meter      metric.MeterProvider
	Propagator propagation.TextMapPropagator
	Metrics    http.Handler // Prometheus exposition for the admin server

	exporting bool
	shutdown  []func(context.Context) error
}

// Exporting reports whether cfg turns export on.
func Exporting(cfg Config) bool {
	switch strings.ToLower(cfg.Enabled) {
	case "true":
		return true
	case "false":
		return false
	}
	return cfg.Environment == "production" || cfg.Endpoint != ""
}

// Setup builds the providers. Metrics are always collected locally; traces
// are recorded and exported only when Exporting(cfg).
func Setup(ctx context.Context, cfg Config) (*Providers, error) {
	proto := strings.ToLower(cfg.Protocol)
	if proto == "" {
		proto = "grpc"
	}
	if proto != "grpc" && proto != "http" {
		return nil, fmt.Errorf("otel: OTEL_PROTOCOL must be grpc or http, got %q", cfg.Protocol)
	}
	exporting := Exporting(cfg)

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.Version),
		attribute.String("deployment.environment.name", cfg.Environment),
	))
	if err != nil {
		return nil, fmt.Errorf("otel: resource: %w", err)
	}

	p := &Providers{
		Propagator: propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
		exporting:  exporting,
	}

	// Metrics: Prometheus always, OTLP when exporting.
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	promReader, err := promexporter.New(promexporter.WithRegisterer(reg))
	if err != nil {
		return nil, fmt.Errorf("otel: prometheus exporter: %w", err)
	}
	mopts := []sdkmetric.Option{sdkmetric.WithResource(res), sdkmetric.WithReader(promReader)}
	if exporting {
		exp, err := newMetricExporter(ctx, cfg, proto)
		if err != nil {
			return nil, err
		}
		mopts = append(mopts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)))
	}
	mp := sdkmetric.NewMeterProvider(mopts...)
	p.Meter = mp
	p.Metrics = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	p.shutdown = append(p.shutdown, mp.Shutdown)

	// Traces: real provider only when exporting; otherwise noop, but the
	// propagator still carries incoming context through the service.
	if exporting {
		exp, err := newTraceExporter(ctx, cfg, proto)
		if err != nil {
			return nil, err
		}
		tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
		p.Tracer = tp
		p.shutdown = append(p.shutdown, tp.Shutdown)
	} else {
		p.Tracer = tracenoop.NewTracerProvider()
	}

	otel.SetTracerProvider(p.Tracer)
	otel.SetMeterProvider(p.Meter)
	otel.SetTextMapPropagator(p.Propagator)
	return p, nil
}

// otlpOptions builds the exporter options shared by every OTLP protocol.
func otlpOptions[O any](cfg Config, withEndpoint func(string) O, withInsecure O) []O {
	var opts []O
	if cfg.Endpoint != "" {
		opts = append(opts, withEndpoint(cfg.Endpoint))
	}
	if cfg.Insecure {
		opts = append(opts, withInsecure)
	}
	return opts
}

func newTraceExporter(ctx context.Context, cfg Config, proto string) (*otlptrace.Exporter, error) {
	var (
		exp *otlptrace.Exporter
		err error
	)
	if proto == "http" {
		exp, err = otlptracehttp.New(ctx, otlpOptions(cfg, otlptracehttp.WithEndpoint, otlptracehttp.WithInsecure())...)
	} else {
		exp, err = otlptracegrpc.New(ctx, otlpOptions(cfg, otlptracegrpc.WithEndpoint, otlptracegrpc.WithInsecure())...)
	}
	if err != nil {
		return nil, fmt.Errorf("otel: trace exporter: %w", err)
	}
	return exp, nil
}

func newMetricExporter(ctx context.Context, cfg Config, proto string) (sdkmetric.Exporter, error) {
	var (
		exp sdkmetric.Exporter
		err error
	)
	if proto == "http" {
		exp, err = otlpmetrichttp.New(ctx, otlpOptions(cfg, otlpmetrichttp.WithEndpoint, otlpmetrichttp.WithInsecure())...)
	} else {
		exp, err = otlpmetricgrpc.New(ctx, otlpOptions(cfg, otlpmetricgrpc.WithEndpoint, otlpmetricgrpc.WithInsecure())...)
	}
	if err != nil {
		return nil, fmt.Errorf("otel: metric exporter: %w", err)
	}
	return exp, nil
}

// Exporting reports whether traces and metrics are exported over OTLP.
func (p *Providers) Exporting() bool { return p.exporting }

// Shutdown flushes and stops every provider.
func (p *Providers) Shutdown(ctx context.Context) error {
	var errs []error
	for _, fn := range p.shutdown {
		if err := fn(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("otel: shutdown: %w", err)
	}
	return nil
}

// LogAttrs returns trace_id and span_id for the span in ctx, or nil. Pass it
// to log.WithContextAttrs so every record inside a span carries both.
func LogAttrs(ctx context.Context) []slog.Attr {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []slog.Attr{
		slog.String(log.KeyTraceID, sc.TraceID().String()),
		slog.String(log.KeySpanID, sc.SpanID().String()),
	}
}
