package runtime

import (
	"context"
	"net/http"

	goahttp "goa.design/goa/v3/http"
)

type RouteMatcher struct {
	mux goahttp.Muxer
}

type routeMatchState struct {
	name *string
	vars *map[string]string
}

type routeMatchKey struct{}

func NewRouteMatcher(endpoints []Endpoint) *RouteMatcher {
	mux := goahttp.NewMuxer()
	rm := &RouteMatcher{mux: mux}

	for _, ep := range endpoints {
		endpointName := ep.Name
		mux.Handle(ep.Method, ep.Pattern, func(w http.ResponseWriter, r *http.Request) {
			st, _ := r.Context().Value(routeMatchKey{}).(*routeMatchState)
			if st == nil || st.name == nil {
				return
			}
			if *st.name != "" {
				return
			}
			*st.name = endpointName
			if st.vars != nil {
				*st.vars = mux.Vars(r)
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}

	if chiMux, ok := mux.(interface{ NotFound(http.HandlerFunc) }); ok {
		chiMux.NotFound(func(http.ResponseWriter, *http.Request) {})
	}

	return rm
}

func (rm *RouteMatcher) Match(r *http.Request) (endpointName string, vars map[string]string, ok bool) {
	if rm == nil || rm.mux == nil || r == nil {
		return "", nil, false
	}
	var name string
	var v map[string]string
	st := &routeMatchState{name: &name, vars: &v}
	ctx := context.WithValue(r.Context(), routeMatchKey{}, st)
	rm.mux.ServeHTTP(noopResponseWriter{}, r.WithContext(ctx))
	if name == "" {
		return "", nil, false
	}
	return name, v, true
}

type noopResponseWriter struct{}

func (noopResponseWriter) Header() http.Header       { return http.Header{} }
func (noopResponseWriter) Write([]byte) (int, error) { return 0, nil }
func (noopResponseWriter) WriteHeader(int)           {}

