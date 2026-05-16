package telemetry

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/langgenius/dify-sandbox/internal/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

// Init sets up OpenTelemetry tracing according to config (env can still override).
// Returns a shutdown function. Safe to call multiple times.
func Init(ctx context.Context, serviceName string, cfg types.DifySandboxGlobalConfigurations) (func(context.Context) error, error) {
	// Map config to env so exporter honors endpoints/headers without relying on option nuances.
	if cfg.Otel.BaseEndpoint != "" {
		_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", cfg.Otel.BaseEndpoint)
	}
	if cfg.Otel.TraceEndpoint != "" {
		_ = os.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", cfg.Otel.TraceEndpoint)
	}
	if cfg.Otel.Protocol != "" {
		_ = os.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", cfg.Otel.Protocol)
	}
	if cfg.Otel.APIKey != "" {
		// Append Authorization header for OTLP/HTTP exporters
		_ = os.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer "+cfg.Otel.APIKey)
	}

	var (
		exp tracesdk.SpanExporter
		err error
	)
	if cfg.Otel.ExporterType == "stdout" {
		exp, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
	} else {
		client := otlptracehttp.NewClient()
		exp, err = otlptrace.New(ctx, client)
	}
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	// Batcher options from config
	batchOpts := []tracesdk.BatchSpanProcessorOption{
		tracesdk.WithMaxExportBatchSize(cfg.Otel.MaxExportBatchSize),
		tracesdk.WithExportTimeout(time.Duration(cfg.Otel.BatchExportTimeoutMS) * time.Millisecond),
		tracesdk.WithMaxQueueSize(cfg.Otel.MaxQueueSize),
	}
	if cfg.Otel.BatchScheduleDelayMS > 0 {
		batchOpts = append(batchOpts, tracesdk.WithBatchTimeout(time.Duration(cfg.Otel.BatchScheduleDelayMS)*time.Millisecond))
	}

	// Sampler
	sampler := tracesdk.ParentBased(tracesdk.TraceIDRatioBased(cfg.Otel.SamplingRate))

	var tp *tracesdk.TracerProvider
	if cfg.Otel.ExporterType == "stdout" {
		// Synchronous processor prints immediately to stdout
		tp = tracesdk.NewTracerProvider(
			tracesdk.WithSyncer(exp),
			tracesdk.WithResource(res),
			tracesdk.WithSampler(sampler),
		)
	} else {
		tp = tracesdk.NewTracerProvider(
			tracesdk.WithBatcher(exp, batchOpts...),
			tracesdk.WithResource(res),
			tracesdk.WithSampler(sampler),
		)
	}
	otel.SetTracerProvider(tp)

	// Use W3C TraceContext + Baggage by default to extract upstream trace id.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Emit a tiny startup span to trigger exporter and aid verification
	fmt.Println("[otel] enabled exporter:", cfg.Otel.ExporterType)
	tr := otel.Tracer("dify-sandbox/telemetry")
	_, s := tr.Start(ctx, "startup.check")
	s.SetAttributes(attribute.String("service.name", serviceName))
	s.End()
	// Force flush to ensure stdout exporter writes before proceeding
	_ = tp.ForceFlush(ctx)

	return tp.Shutdown, nil
}
