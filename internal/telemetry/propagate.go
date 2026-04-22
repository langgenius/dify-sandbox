package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// InjectTraceEnv serializes the current context into W3C headers and returns them
// as environment variables (TRACEPARENT, BAGGAGE). Child processes can read them
// if instrumented accordingly.
func InjectTraceEnv(ctx context.Context) []string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	vars := []string{}
	if tp, ok := carrier["traceparent"]; ok && tp != "" {
		vars = append(vars, "TRACEPARENT="+tp)
	}
	if bg, ok := carrier["baggage"]; ok && bg != "" {
		vars = append(vars, "BAGGAGE="+bg)
	}
	return vars
}