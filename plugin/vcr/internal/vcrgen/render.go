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
		codegen.SimpleImport("net/url"),

		codegen.NewImport("vcrruntime", "github.com/xeger/goa-vcr/runtime"),
		codegen.NewImport("goahttp", "goa.design/goa/v3/http"),
		codegen.NewImport("goa", "goa.design/goa/v3/pkg"),
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
			Name:    "vcr",
			Source:  vcrTmpl,
			FuncMap: func() map[string]any {
				fm := codegen.TemplateFuncs()
				fm["routesCount"] = routesCount
				return fm
			}(),
			Data:    spec,
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

// Scenario is a typed wrapper around vcrruntime.Scenario.
type Scenario struct {
	vcrruntime.Scenario
}

// NewScenario returns a new scenario queue.
func NewScenario() Scenario {
	return Scenario{Scenario: vcrruntime.NewScenario()}
}

// ScenarioFactory creates a Scenario from a loopback-generated Goa HTTP client.
// Implementations can close over the client to fetch unary data.
type ScenarioFactory func(client *httpclient.Client) Scenario

type loopbackDoer struct {
	base goahttp.Doer
}

func (d loopbackDoer) Do(req *http.Request) (*http.Response, error) {
	if req != nil {
		req.Header.Set(vcrruntime.LoopbackHeader, "1")
	}
	return d.base.Do(req)
}

// NewLoopbackClient constructs a service HTTP client pointing at baseURL.
// The returned client always sets vcrruntime.LoopbackHeader on requests.
func NewLoopbackClient(baseURL string, doer goahttp.Doer) (*httpclient.Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid base URL %q", baseURL)
	}
	if doer == nil {
		doer = http.DefaultClient
	}
	doer = loopbackDoer{base: doer}
	{{- if .HasWebSocket }}
	return httpclient.NewClient(
		u.Scheme,
		u.Host,
		doer,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
		nil,
		nil,
	), nil
	{{- else }}
	return httpclient.NewClient(
		u.Scheme,
		u.Host,
		doer,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false,
	), nil
	{{- end }}
}

// BuildScenario constructs a loopback HTTP client (whose requests bypass unary
// scenario handling completely) and applies the factory.
func BuildScenario(baseURL string, doer goahttp.Doer, factory ScenarioFactory) (Scenario, *httpclient.Client, error) {
	client, err := NewLoopbackClient(baseURL, doer)
	if err != nil {
		return Scenario{}, nil, err
	}
	return factory(client), client, nil
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

// Background uses a stub-backed HTTP client to decode stubbed responses into
// typed Goa results.
type Background struct {
	client *httpclient.Client
}

func NewBackground(store *vcrruntime.VCR) *Background {
	doer := vcrruntime.NewStubDoer(store, Endpoints())
	// The scheme/host are irrelevant as StubDoer matches on verb+path.
	scheme := "http"
	host := "vcr.local"
	{{- if .HasWebSocket }}
	return &Background{client: httpclient.NewClient(scheme, host, doer, goahttp.RequestEncoder, goahttp.ResponseDecoder, false, nil, nil)}
	{{- else }}
	return &Background{client: httpclient.NewClient(scheme, host, doer, goahttp.RequestEncoder, goahttp.ResponseDecoder, false)}
	{{- end }}
}

// PlaybackOptions configures playback handler generation.
type PlaybackOptions struct {
	ScenarioName string
}

// NewPlaybackHandler returns a handler that serves stub-backed responses using
// Goa-generated HTTP server code, dispatching to scenario handlers when present.
func NewPlaybackHandler(store *vcrruntime.VCR, scenario Scenario, opts PlaybackOptions) (http.Handler, error) {
	if store == nil {
		return nil, errors.New("vcr: nil store")
	}
	bg := NewBackground(store)
	mux := goahttp.NewMuxer()

	eps := &{{ .ServicePkgName }}.Endpoints{
		{{- range .Endpoints }}
		{{ .MethodVarName }}: makeEndpoint{{ .MethodVarName }}(store, scenario, bg, opts),
		{{- end }}
	}

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

	// Mark loopback requests so endpoint dispatch can avoid scenario recursion.
	return vcrruntime.LoopbackMiddleware(mux), nil
}

{{ range .Endpoints }}

// Service{{ .MethodVarName }}Func is the typed scenario handler signature for {{ .MethodVarName }}.
{{- if .IsStreaming }}
type Service{{ .MethodVarName }}Func func(context.Context, {{ .PayloadRef }}, {{ $.ServicePkgName }}.{{ .MethodVarName }}ServerStream) error
{{- else if .ResultRef }}
type Service{{ .MethodVarName }}Func func(context.Context, {{ .PayloadRef }}) ({{ .ResultRef }}, error)
{{- else }}
type Service{{ .MethodVarName }}Func func(context.Context, {{ .PayloadRef }}) error
{{- end }}

func (s *Scenario) Set{{ .MethodVarName }}(f Service{{ .MethodVarName }}Func) {
	s.Set("{{ .MethodVarName }}", f)
}

func (s *Scenario) Add{{ .MethodVarName }}(f Service{{ .MethodVarName }}Func) {
	s.Add("{{ .MethodVarName }}", f)
}

{{ if .IsStreaming }}
func makeEndpoint{{ .MethodVarName }}(_ *vcrruntime.VCR, scenario Scenario, bg *Background, _ PlaybackOptions) goa.Endpoint {
	_ = bg
	return func(ctx context.Context, v any) (any, error) {
		in, ok := v.(*{{ $.ServicePkgName }}.{{ .MethodVarName }}EndpointInput)
		if !ok || in == nil {
			return nil, fmt.Errorf("vcr: unexpected {{ .MethodVarName }} input %T", v)
		}
		handler := scenario.Next("{{ .MethodVarName }}")
		if handler == nil {
			return nil, fmt.Errorf("vcr: no scenario handler for {{ .MethodVarName }}")
		}
		f, ok := handler.(Service{{ .MethodVarName }}Func)
		if !ok {
			return nil, fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", handler)
		}
		return nil, f(ctx, in.Payload, in.Stream)
	}
}
{{ else if and .ResultRef .ViewedResultInitName }}
func (b *Background) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) ({{ .ResultRef }}, error) {
	var zero {{ .ResultRef }}
	ep := b.client.{{ .MethodVarName }}()
	res, err := ep(ctx, p)
	if err != nil {
		return zero, err
	}
	typed, ok := res.({{ .ResultRef }})
	if !ok {
		return zero, fmt.Errorf("vcr: unexpected {{ .MethodVarName }} response %T", res)
	}
	return typed, nil
}

func makeEndpoint{{ .MethodVarName }}(_ *vcrruntime.VCR, scenario Scenario, bg *Background, _ PlaybackOptions) goa.Endpoint {
	return func(ctx context.Context, v any) (any, error) {
		p, ok := v.({{ .PayloadRef }})
		if !ok {
			return nil, fmt.Errorf("vcr: unexpected {{ .MethodVarName }} payload %T", v)
		}

		var (
			res {{ .ResultRef }}
			err error
		)

		if vcrruntime.IsLoopback(ctx) {
			res, err = bg.{{ .MethodVarName }}(ctx, p)
		} else {
			handler := scenario.Next("{{ .MethodVarName }}")
			if handler != nil {
				f, ok := handler.(Service{{ .MethodVarName }}Func)
				if !ok {
					return nil, fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", handler)
				}
				res, err = f(ctx, p)
			} else {
				res, err = bg.{{ .MethodVarName }}(ctx, p)
			}
		}
		if err != nil {
			return nil, err
		}

		{{- if .ViewedResultViewName }}
		return {{ $.ServicePkgName }}.{{ .ViewedResultInitName }}(res, {{ printf "%q" .ViewedResultViewName }}), nil
		{{- else }}
		return {{ $.ServicePkgName }}.{{ .ViewedResultInitName }}(res, viewFromPayload(p)), nil
		{{- end }}
	}
}
{{ else if .ResultRef }}
func (b *Background) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) ({{ .ResultRef }}, error) {
	var zero {{ .ResultRef }}
	ep := b.client.{{ .MethodVarName }}()
	res, err := ep(ctx, p)
	if err != nil {
		return zero, err
	}
	typed, ok := res.({{ .ResultRef }})
	if !ok {
		return zero, fmt.Errorf("vcr: unexpected {{ .MethodVarName }} response %T", res)
	}
	return typed, nil
}

func makeEndpoint{{ .MethodVarName }}(_ *vcrruntime.VCR, scenario Scenario, bg *Background, _ PlaybackOptions) goa.Endpoint {
	return func(ctx context.Context, v any) (any, error) {
		p, ok := v.({{ .PayloadRef }})
		if !ok {
			return nil, fmt.Errorf("vcr: unexpected {{ .MethodVarName }} payload %T", v)
		}
		if vcrruntime.IsLoopback(ctx) {
			return bg.{{ .MethodVarName }}(ctx, p)
		}
		handler := scenario.Next("{{ .MethodVarName }}")
		if handler != nil {
			f, ok := handler.(Service{{ .MethodVarName }}Func)
			if !ok {
				return nil, fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", handler)
			}
			return f(ctx, p)
		}
		return bg.{{ .MethodVarName }}(ctx, p)
	}
}
{{ else }}
func (b *Background) {{ .MethodVarName }}(ctx context.Context, p {{ .PayloadRef }}) error {
	ep := b.client.{{ .MethodVarName }}()
	_, err := ep(ctx, p)
	return err
}

func makeEndpoint{{ .MethodVarName }}(_ *vcrruntime.VCR, scenario Scenario, bg *Background, _ PlaybackOptions) goa.Endpoint {
	return func(ctx context.Context, v any) (any, error) {
		p, ok := v.({{ .PayloadRef }})
		if !ok {
			return nil, fmt.Errorf("vcr: unexpected {{ .MethodVarName }} payload %T", v)
		}
		if vcrruntime.IsLoopback(ctx) {
			return nil, bg.{{ .MethodVarName }}(ctx, p)
		}
		handler := scenario.Next("{{ .MethodVarName }}")
		if handler != nil {
			f, ok := handler.(Service{{ .MethodVarName }}Func)
			if !ok {
				return nil, fmt.Errorf("vcr: scenario handler for {{ .MethodVarName }} has unexpected type %T", handler)
			}
			return nil, f(ctx, p)
		}
		return nil, bg.{{ .MethodVarName }}(ctx, p)
	}
}
{{ end }}

{{ end }}
`

func routesCount(endpoints []EndpointSpec) int {
	n := 0
	for _, ep := range endpoints {
		n += len(ep.Routes)
	}
	return n
}
