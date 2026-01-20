package log

import "context"

type traceContextKey struct{}
type identityContextKey struct{}

type TraceContext struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}

type Identity struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	UserType string `json:"user_type"`
}

func WithTrace(ctx context.Context, tc TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, tc)
}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, id)
}

func TraceFromContext(ctx context.Context) (TraceContext, bool) {
	if ctx == nil {
		return TraceContext{}, false
	}
	tc, ok := ctx.Value(traceContextKey{}).(TraceContext)
	return tc, ok
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	if ctx == nil {
		return Identity{}, false
	}
	id, ok := ctx.Value(identityContextKey{}).(Identity)
	return id, ok
}

func EnsureTrace(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := TraceFromContext(ctx); !ok {
		ctx = WithTrace(ctx, TraceContext{
			TraceID: GenerateTraceID(),
			SpanID:  GenerateSpanID(),
		})
	}
	return ctx
}
