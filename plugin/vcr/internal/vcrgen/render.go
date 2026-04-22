package vcrgen

import (
	"path/filepath"
	"sort"

	"goa.design/goa/v3/codegen"
)

func RenderServiceVCR(spec ServiceSpec) *codegen.File {
	p := filepath.Join(codegen.Gendir, "http", spec.ServicePathName, "vcr", "vcr.go")

	imports := []*codegen.ImportSpec{
		codegen.SimpleImport("context"),
		codegen.SimpleImport("errors"),
		codegen.SimpleImport("fmt"),
		codegen.SimpleImport("net/http"),

		codegen.NewImport("vcrruntime", "github.com/xeger/goa-vcr/runtime"),
		codegen.NewImport("goahttp", "goa.design/goa/v3/http"),
		codegen.NewImport(spec.ServicePkgName, filepath.ToSlash(filepath.Join(spec.GenPkg, spec.ServicePathName))),
		codegen.NewImport("httpclient", filepath.ToSlash(filepath.Join(spec.GenPkg, "http", spec.ServicePathName, "client"))),
		codegen.NewImport("httpserver", filepath.ToSlash(filepath.Join(spec.GenPkg, "http", spec.ServicePathName, "server"))),
	}

	if spec.HasViewedResult {
		imports = append(imports, codegen.SimpleImport("reflect"))
	}
	if spec.HasWebSocket {
		imports = append(imports, codegen.SimpleImport("github.com/gorilla/websocket"))
	}

	// Keep imports stable for unit tests / diffs.
	sort.SliceStable(imports, func(i, j int) bool {
		if imports[i].Path == imports[j].Path {
			return imports[i].Name < imports[j].Name
		}
		return imports[i].Path < imports[j].Path
	})

	sections := []*codegen.SectionTemplate{
		codegen.Header("vcr", "vcr", imports),
		{
			Name:   "vcr",
			Source: vcrTmpl,
			FuncMap: func() map[string]any {
				fm := codegen.TemplateFuncs()
				fm["routesCount"] = routesCount
				return fm
			}(),
			Data: spec,
		},
	}

	return &codegen.File{Path: p, SectionTemplates: sections}
}

const vcrTmpl = `

// Endpoints returns the HTTP mountpoints for the service. It is used for
// request-to-endpoint matching when serving stubs.
func Endpoints() []vcrruntime.Endpoint {
	endpoints := make([]vcrruntime.Endpoint, 0, {{ routesCount .Endpoints }})
	{{- range .Endpoints }}
		{{- $m := .MethodVarName }}
		{{- range .Routes }}
	endpoints = append(endpoints, vcrruntime.Endpoint{
		Name:    {{ printf "%q" $m }},
		Method:  {{ printf "%q" .Verb }},
		Pattern: {{ printf "%q" .Path }},
	})
		{{- end }}
	{{- end }}
	return endpoints
}

// Scenario implements {{ .ServicePkgName }}.Service by maintaining a
// clue/mock-backed handler queue per method and delegating to an underlying
// "next" Service when no handler is set. Scenarios compose by wrapping other
// Services, enabling middleware-style stacking over the stub-backed background.
type Scenario struct {
	queue vcrruntime.Scenario
	next  {{ .ServicePkgName }}.Service
}

// NewScenario returns a new Scenario layered on next.
func NewScenario(next {{ .ServicePkgName }}.Service) *Scenario {
	return &Scenario{queue: vcrruntime.NewScenario(), next: next}
}

// Stack applies layers to bg with the first layer outermost and the last
// layer innermost. Stack(bg, outer, middle, inner) produces
// outer(middle(inner(bg))).
func Stack(bg {{ .ServicePkgName }}.Service, layers ...func({{ .ServicePkgName }}.Service) {{ .ServicePkgName }}.Service) {{ .ServicePkgName }}.Service {
	result := bg
	for i := len(layers) - 1; i >= 0; i-- {
		result = layers[i](result)
	}
	return result
}

{{- if .HasViewedResult }}
func viewFromPayload(p any) string {
	const def = "default"
	if p == nil {
		return def
	}
	rv := reflect.ValueOf(p)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return def
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return def
	}
	f := rv.FieldByName("View")
	if !f.IsValid() || f.Kind() != reflect.String {
		return def
	}
	if v := f.String(); v != "" {
		return v
	}
	return def
}
{{- end }}

// backgroundService implements {{ .ServicePkgName }}.Service using a
// stub-backed Goa HTTP client for unary methods. Streaming methods return an
// explicit error — no recorded-stream background exists.
type backgroundService struct {
	hc *{{ .ServicePkgName }}.Client
}

// NewBackground returns a {{ .ServicePkgName }}.Service backed by recorded
// stubs in store.
func NewBackground(store *vcrruntime.VCR) {{ .ServicePkgName }}.Service {
	doer := vcrruntime.NewStubDoer(store, Endpoints())
	// The scheme/host are irrelevant as StubDoer matches on verb+path.
	scheme := "http"
	host := "vcr.local"
	{{- if .HasWebSocket }}
	hc := httpclient.NewClient(scheme, host, doer, goahttp.RequestEncoder, goahttp.ResponseDecoder, false, nil, nil)
	{{- else }}
	hc := httpclient.NewClient(scheme, host, doer, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)
	{{- end }}
	return &backgroundService{hc: &{{ .ServicePkgName }}.Client{
		{{- range .Endpoints }}
		{{ .MethodVarName }}Endpoint: hc.{{ .MethodVarName }}(),
		{{- end }}
	}}
}

// NewPlaybackHandler wraps any {{ .ServicePkgName }}.Service as an HTTP
// handler using the Goa-generated HTTP server for the service.
func NewPlaybackHandler(svc {{ .ServicePkgName }}.Service) (http.Handler, error) {
	if svc == nil {
		return nil, errors.New("vcr: nil service")
	}
	mux := goahttp.NewMuxer()

	eps := {{ .ServicePkgName }}.NewEndpoints(svc)

	errHandler := func(ctx context.Context, w http.ResponseWriter, err error) {
		// Keep this minimal: callers may install their own goa error formatter higher up.
		_ = ctx
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	{{- if .HasWebSocket }}
	upgrader := &websocket.Upgrader{
		Subprotocols: []string{"ws", "wss", "auth.bearer"},
		CheckOrigin:  func(*http.Request) bool { return true },
	}
	server := httpserver.New(eps, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, errHandler, nil, upgrader, nil)
	{{- else }}
	server := httpserver.New(eps, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, errHandler, nil)
	{{- end }}
	server.Mount(mux)

	return mux, nil
}

{{- range .Endpoints }}

// Service{{ .MethodVarName }}Func is the typed scenario handler signature for {{ .MethodVarName }}.
{{- if .IsStreaming }}
type Service{{ .MethodVarName }}Func func(context.Context, {{ .PayloadRef }}, {{ $.ServicePkgName }}.{{ .MethodVarName }}ServerStream) error
{{- else if .ResultRef }}
type Service{{ .MethodVarName }}Func func(context.Context, {{ .PayloadRef }}, {{ $.ServicePkgName }}.Service) ({{ .ResultRef }}, error)
{{- else }}
type Service{{ .MethodVarName }}Func func(context.Context, {{ .PayloadRef }}, {{ $.ServicePkgName }}.Service) error
{{- end }}

func (s *Scenario) Set{{ .MethodVarName }}(f Service{{ .MethodVarName }}Func) {
	s.queue.Set("{{ .MethodVarName }}", f)
}

func (s *Scenario) Add{{ .MethodVarName }}(f Service{{ .MethodVarName }}Func) {
	s.queue.Add("{{ .MethodVarName }}", f)
}

{{ if .IsStreaming }}
// {{ .MethodVarName }} dispatches to the handler queue (if any) or delegates
// to the underlying Service. Stream handlers are terminal — they do not
// receive a next argument.
func (s *Scenario) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}, stream {{ $.ServicePkgName }}.{{ .MethodVarName }}ServerStream) error {
	if h := s.queue.Next("{{ .MethodVarName }}"); h != nil {
		fn, ok := h.(Service{{ .MethodVarName }}Func)
		if !ok {
			return fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", h)
		}
		return fn(ctx, p, stream)
	}
	return s.next.{{ .MethodVarName }}(ctx, p, stream)
}

func (b *backgroundService) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}, stream {{ $.ServicePkgName }}.{{ .MethodVarName }}ServerStream) error {
	return fmt.Errorf("vcr: no scenario handler for {{ .MethodVarName }} and no recorded-stream background is available")
}
{{ else if and .ResultRef .ViewedResultInitName }}
// {{ .MethodVarName }} dispatches to the handler queue (if any) or delegates.
// Handlers return the plain result type and the selected view name; the Goa
// endpoint layer (NewEndpoints) is responsible for wrapping to the viewed type.
func (s *Scenario) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) ({{ .ResultRef }}, string, error) {
	if h := s.queue.Next("{{ .MethodVarName }}"); h != nil {
		fn, ok := h.(Service{{ .MethodVarName }}Func)
		if !ok {
			return nil, "", fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", h)
		}
		res, err := fn(ctx, p, s.next)
		if err != nil {
			return nil, "", err
		}
		{{- if .ViewedResultViewName }}
		return res, {{ printf "%q" .ViewedResultViewName }}, nil
		{{- else }}
		return res, viewFromPayload(p), nil
		{{- end }}
	}
	return s.next.{{ .MethodVarName }}(ctx, p)
}

func (b *backgroundService) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) ({{ .ResultRef }}, string, error) {
	res, err := b.hc.{{ .MethodVarName }}(ctx, p)
	if err != nil {
		return nil, "", err
	}
	{{- if .ViewedResultViewName }}
	return res, {{ printf "%q" .ViewedResultViewName }}, nil
	{{- else }}
	return res, viewFromPayload(p), nil
	{{- end }}
}
{{ else if .ResultRef }}
func (s *Scenario) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) ({{ .ResultRef }}, error) {
	if h := s.queue.Next("{{ .MethodVarName }}"); h != nil {
		fn, ok := h.(Service{{ .MethodVarName }}Func)
		if !ok {
			return nil, fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", h)
		}
		return fn(ctx, p, s.next)
	}
	return s.next.{{ .MethodVarName }}(ctx, p)
}

func (b *backgroundService) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) ({{ .ResultRef }}, error) {
	return b.hc.{{ .MethodVarName }}(ctx, p)
}
{{ else }}
func (s *Scenario) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) error {
	if h := s.queue.Next("{{ .MethodVarName }}"); h != nil {
		fn, ok := h.(Service{{ .MethodVarName }}Func)
		if !ok {
			return fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", h)
		}
		err := fn(ctx, p, s.next)
		return err
	}
	return s.next.{{ .MethodVarName }}(ctx, p)
}

func (b *backgroundService) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) error {
	return b.hc.{{ .MethodVarName }}(ctx, p)
}
{{ end }}
{{- end }}
`

func routesCount(endpoints []EndpointSpec) int {
	n := 0
	for _, ep := range endpoints {
		n += len(ep.Routes)
	}
	return n
}
