// Package telemetry provides optional OpenTelemetry tracing for anyctl. It is
// off by default and pays ZERO cost unless the standard OTEL_* env configures an
// OTLP endpoint — an unconfigured `anyctl radarr list` stays curl-fast. When an
// endpoint is set, each invocation emits one span (service, command, method,
// status, duration) so back-to-back and parallel-agent calls are traceable in
// Tempo. Shutdown flushes with a short timeout so a slow collector can never
// hang the CLI.
//
// The CLI emits one span per invocation; the MCP server reuses the same
// provider and emits one span per tool call. Metrics remain future work.
package telemetry

import (
	"context"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// flushTimeout bounds how long shutdown waits on the collector before giving up.
const flushTimeout = 2 * time.Second

// Enabled reports whether OTLP trace export is configured via standard env.
func Enabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

// Start returns a Tracer and a shutdown func. When tracing is unconfigured (or
// the exporter fails to build), it returns a no-op tracer and a no-op shutdown —
// telemetry never blocks or breaks a command (fail-open). version stamps the
// resource's service.version.
func Start(ctx context.Context, version string) (trace.Tracer, func()) {
	noopTracer := noop.NewTracerProvider().Tracer("anyctl")
	if !Enabled() {
		return noopTracer, func() {}
	}
	exp, err := newExporter(ctx)
	if err != nil {
		return noopTracer, func() {}
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", serviceName()),
		attribute.String("service.version", version),
	))
	if err != nil {
		res = resource.Default()
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
		defer cancel()
		_ = tp.Shutdown(ctx)
	}
	return tp.Tracer("anyctl"), shutdown
}

// newExporter selects the OTLP protocol from OTEL_EXPORTER_OTLP_PROTOCOL
// (grpc | http/protobuf). Per the OTel spec the default is http/protobuf.
func newExporter(ctx context.Context) (*otlptrace.Exporter, error) {
	switch strings.ToLower(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")) {
	case "grpc":
		return otlptracegrpc.New(ctx)
	default: // "", "http/protobuf", "http/json"
		return otlptracehttp.New(ctx)
	}
}

func serviceName() string {
	if n := os.Getenv("OTEL_SERVICE_NAME"); n != "" {
		return n
	}
	return "anyctl"
}
