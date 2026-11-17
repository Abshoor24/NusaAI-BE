package provider

import (
	"context"
	"net/http"
	"net/http/httptrace"
	"time"
)

type firstByteHookKey struct{}

// WithFirstByteHook attaches a temporary diagnostic callback that fires when
// the transport reads the first response byte (httptrace.GotFirstResponseByte).
// Without a hook the request path is unchanged. Used only by cmd/aibench.
func WithFirstByteHook(ctx context.Context, fn func(time.Time)) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, firstByteHookKey{}, fn)
}

func firstByteHook(ctx context.Context) func(time.Time) {
	fn, _ := ctx.Value(firstByteHookKey{}).(func(time.Time))
	return fn
}

func traceFirstByte(ctx context.Context, req *http.Request) *http.Request {
	fn := firstByteHook(ctx)
	if fn == nil {
		return req
	}
	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() { fn(time.Now()) },
	}
	return req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
}
