package otel_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/guilhermebr/gox/pkg/log"
	"github.com/guilhermebr/gox/pkg/otel"
)

func TestExportDecision(t *testing.T) {
	tests := []struct {
		name string
		cfg  otel.Config
		want bool
	}{
		{"auto, development, no endpoint", otel.Config{Enabled: "auto", Environment: "development"}, false},
		{"auto, development, endpoint set", otel.Config{Enabled: "auto", Environment: "development", Endpoint: "localhost:4317"}, true},
		{"auto, production", otel.Config{Enabled: "auto", Environment: "production"}, true},
		{"false, production with endpoint", otel.Config{Enabled: "false", Environment: "production", Endpoint: "x:1"}, false},
		{"true, development", otel.Config{Enabled: "true", Environment: "development"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := otel.Exporting(tt.cfg); got != tt.want {
				t.Fatalf("Exporting = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSetupWithoutExportStillServesMetricsAndPropagates(t *testing.T) {
	p, err := otel.Setup(context.Background(), otel.Config{
		ServiceName: "billing", Version: "1.0.0", Environment: "development", Enabled: "auto", Protocol: "grpc",
	})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := p.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	}()
	if p.Exporting() {
		t.Fatal("development without an endpoint must not export")
	}

	// Metrics are always local: a counter shows up on the Prometheus handler.
	counter, err := p.Meter.Meter("test").Int64Counter("gox_test_hits_total")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(context.Background(), 3)
	rec := httptest.NewRecorder()
	p.Metrics.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), "gox_test_hits_total") {
		t.Fatalf("/metrics = %d\n%s", rec.Code, body)
	}
	if !strings.Contains(string(body), `service_name="billing"`) && !strings.Contains(string(body), "target_info") {
		t.Fatalf("resource attributes missing from /metrics:\n%s", body)
	}

	// Spans are not recorded when not exporting, but context still propagates.
	_, span := p.Tracer.Tracer("test").Start(context.Background(), "op")
	if span.IsRecording() {
		t.Fatal("spans must not be recorded when export is off")
	}
	span.End()
	fields := strings.Join(p.Propagator.Fields(), ",")
	if !strings.Contains(fields, "traceparent") || !strings.Contains(fields, "baggage") {
		t.Fatalf("Propagator fields = %q, want W3C trace context and baggage", fields)
	}
}

func TestSetupWithHTTPEndpointExports(t *testing.T) {
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer collector.Close()

	p, err := otel.Setup(context.Background(), otel.Config{
		ServiceName: "billing", Environment: "development", Enabled: "true",
		Endpoint: strings.TrimPrefix(collector.URL, "http://"), Protocol: "http", Insecure: true,
	})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if !p.Exporting() {
		t.Fatal("Enabled=true must export")
	}
	_, span := p.Tracer.Tracer("test").Start(context.Background(), "op")
	if !span.IsRecording() {
		t.Fatal("spans must be recorded when exporting")
	}
	span.End()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestSetupRejectsBadProtocol(t *testing.T) {
	_, err := otel.Setup(context.Background(), otel.Config{Enabled: "true", Endpoint: "x:1", Protocol: "carrier-pigeon"})
	if err == nil || !strings.Contains(err.Error(), "OTEL_PROTOCOL") {
		t.Fatalf("err = %v", err)
	}
}

func TestLogAttrsStampsTraceAndSpanIDs(t *testing.T) {
	if got := otel.LogAttrs(context.Background()); got != nil {
		t.Fatalf("LogAttrs without a span = %v", got)
	}
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	defer span.End()

	attrs := otel.LogAttrs(ctx)
	got := map[string]string{}
	for _, a := range attrs {
		got[a.Key] = a.Value.String()
	}
	if got[log.KeyTraceID] != span.SpanContext().TraceID().String() || got[log.KeySpanID] != span.SpanContext().SpanID().String() {
		t.Fatalf("attrs = %v", got)
	}
}
