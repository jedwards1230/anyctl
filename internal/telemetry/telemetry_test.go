package telemetry

import (
	"context"
	"testing"
)

func TestDisabledByDefault(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	if Enabled() {
		t.Fatal("tracing should be disabled with no OTEL_* endpoint")
	}
	tr, shutdown := Start(context.Background(), "test")
	if tr == nil {
		t.Fatal("expected a no-op tracer, got nil")
	}
	// No-op tracer must not panic and shutdown must be safe to call.
	_, span := tr.Start(context.Background(), "x")
	span.End()
	shutdown()
}

func TestEnabledWhenEndpointSet(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	if !Enabled() {
		t.Fatal("tracing should be enabled when endpoint is set")
	}
	// Start must fail-open: a real provider, a working span, a bounded shutdown
	// even if nothing is listening at the endpoint.
	tr, shutdown := Start(context.Background(), "test")
	_, span := tr.Start(context.Background(), "x")
	span.End()
	shutdown() // must return within flushTimeout, not hang
}

func TestServiceName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	if serviceName() != "labctl" {
		t.Fatalf("default service name = %q, want labctl", serviceName())
	}
	t.Setenv("OTEL_SERVICE_NAME", "custom")
	if serviceName() != "custom" {
		t.Fatalf("OTEL_SERVICE_NAME override not honored")
	}
}
