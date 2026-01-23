package runtime

import (
	"context"
	"net/http"
)

// LoopbackHeader identifies requests from the scenario loopback client.
// Playback must treat loopback requests as "bypass scenarios" to avoid recursion.
const LoopbackHeader = "X-Vcr-Loopback"

type loopbackKey struct{}

// WithLoopback marks ctx as coming from a loopback request.
func WithLoopback(ctx context.Context) context.Context {
	if ctx == nil {
		return context.WithValue(context.Background(), loopbackKey{}, true)
	}
	return context.WithValue(ctx, loopbackKey{}, true)
}

// IsLoopback reports whether ctx was marked as a loopback request.
func IsLoopback(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(loopbackKey{}).(bool)
	return v
}

// LoopbackMiddleware tags request contexts when LoopbackHeader is present.
func LoopbackMiddleware(next http.Handler) http.Handler {
	if next == nil {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r != nil && r.Header.Get(LoopbackHeader) != "" {
			r = r.WithContext(WithLoopback(r.Context()))
		}
		next.ServeHTTP(w, r)
	})
}

