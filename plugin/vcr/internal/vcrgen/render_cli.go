package vcrgen

import (
	"path/filepath"
	"sort"

	"goa.design/goa/v3/codegen"
)

func RenderServiceVCRCLI(spec ServiceSpec) *codegen.File {
	p := filepath.Join(codegen.Gendir, "http", spec.ServicePathName, "vcr", "cli.go")

	imports := []*codegen.ImportSpec{
		codegen.SimpleImport("bytes"),
		codegen.SimpleImport("context"),
		codegen.SimpleImport("encoding/json"),
		codegen.SimpleImport("flag"),
		codegen.SimpleImport("fmt"),
		codegen.SimpleImport("io"),
		codegen.SimpleImport("net/http"),
		codegen.SimpleImport("net/http/httputil"),
		codegen.SimpleImport("net/url"),
		codegen.SimpleImport("os"),
		codegen.SimpleImport("os/signal"),
		codegen.SimpleImport("path/filepath"),
		codegen.SimpleImport("strings"),
		codegen.SimpleImport("syscall"),
		codegen.SimpleImport("time"),

		codegen.NewImport("vcrruntime", "github.com/xeger/goa-vcr/runtime"),
		codegen.NewImport("log", "goa.design/clue/log"),
	}

	sort.SliceStable(imports, func(i, j int) bool {
		if imports[i].Path == imports[j].Path {
			return imports[i].Name < imports[j].Name
		}
		return imports[i].Path < imports[j].Path
	})

	sections := []*codegen.SectionTemplate{
		codegen.Header("vcr", "vcr", imports),
		{
			Name:   "cli",
			Source: vcrCLITmpl,
			Data:   spec,
		},
	}

	return &codegen.File{Path: p, SectionTemplates: sections}
}

const vcrCLITmpl = `

var globalDebug bool

// CLIConfig controls the generated CLI behavior and defaults.
type CLIConfig struct {
	AppName          string
	ScenarioRegistry map[string]ScenarioFactory
	DefaultPort      int
	DefaultUpstream  string
	DefaultScenario  string
	DefaultMaxVariants int
}

// Usage returns a full CLI usage string.
func Usage(cfg CLIConfig) string {
	cfg = normalizeCLIConfig(cfg)
	return fmt.Sprintf(
		"Usage: %s [global options] <command> [options]\n\n"+
			"Global options:\n"+
			"  -debug    Enable debug logging\n\n"+
			"Commands:\n"+
			"  play       Serve recorded VCR stubs as an HTTP API\n"+
			"  record     Start a recording proxy to capture new VCR stubs\n"+
			"  refresh    Refresh VCR stubs by re-fetching from upstream endpoints\n\n"+
			"Run '%s <command> -h' for help on a specific command.\n",
		cfg.AppName,
		cfg.AppName,
	)
}

// RunCLI parses args and executes the command, returning an exit code.
func RunCLI(args []string, cfg CLIConfig) int {
	cfg = normalizeCLIConfig(cfg)

	fs := flag.NewFlagSet(cfg.AppName, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.BoolVar(&globalDebug, "debug", false, "Enable debug logging")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, Usage(cfg))
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 1
	}

	rest := fs.Args()
	if len(rest) < 1 {
		fs.Usage()
		return 1
	}

	switch rest[0] {
	case "record":
		return cmdRecord(rest[1:], cfg)
	case "play":
		return cmdPlay(rest[1:], cfg)
	case "refresh":
		return cmdRefresh(rest[1:], cfg)
	case "-h", "--help", "help":
		fs.Usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", rest[0])
		fs.Usage()
		return 1
	}
}

func normalizeCLIConfig(cfg CLIConfig) CLIConfig {
	if cfg.AppName == "" {
		cfg.AppName = "vcr"
	}
	if cfg.DefaultPort == 0 {
		cfg.DefaultPort = 8084
	}
	if cfg.DefaultUpstream == "" {
		cfg.DefaultUpstream = "https://atlaslive.io"
	}
	if cfg.DefaultScenario == "" {
		cfg.DefaultScenario = "Noop"
	}
	if cfg.DefaultMaxVariants == 0 {
		cfg.DefaultMaxVariants = 5
	}
	if cfg.ScenarioRegistry == nil {
		cfg.ScenarioRegistry = map[string]ScenarioFactory{}
	}
	return cfg
}

func cmdContext(cmd string) context.Context {
	ctx := log.Context(
		context.Background(),
		log.WithFormat(log.FormatTerminal),
		log.WithDisableBuffering(func(context.Context) bool { return true }),
	)
	ctx = log.With(ctx, log.KV{K: "cmd", V: cmd})
	if globalDebug {
		ctx = log.Context(ctx, log.WithDebug())
	}
	return ctx
}

// withRequestLogContext ensures request handlers have a clue/log context so
// log.Debug() calls inside generated code or scenarios can emit output.
func withRequestLogContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := log.Context(
			r.Context(),
			log.WithFormat(log.FormatTerminal),
			log.WithDisableBuffering(func(context.Context) bool { return true }),
		)
		if globalDebug {
			ctx = log.Context(ctx, log.WithDebug())
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type respCapture struct {
	w      http.ResponseWriter
	status int
	bytes  int
}

func (c *respCapture) Header() http.Header { return c.w.Header() }

func (c *respCapture) WriteHeader(status int) {
	c.status = status
	c.w.WriteHeader(status)
}

func (c *respCapture) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	n, err := c.w.Write(p)
	c.bytes += n
	return n, err
}

func (c *respCapture) Unwrap() http.ResponseWriter { return c.w }

func vcrAccessLog(store *vcrruntime.VCR) func(http.Handler) http.Handler {
	matcher := vcrruntime.NewRouteMatcher(Endpoints())
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rc := &respCapture{w: w}

			ctx := r.Context()
			start := time.Now()

			method := r.Method
			path := r.URL.Path
			rawQuery := r.URL.RawQuery

			streamType := ""
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				streamType = "ws"
			} else if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
				streamType = "sse"
			}
			loopback := vcrruntime.IsLoopback(ctx)

			endpointName, vars, ok := matcher.Match(r)
			div := ""
			hasStub := false
			if ok {
				div = vcrruntime.RequestDiversifier(store.Policy, endpointName, r.URL.Query(), vars)
				hasStub, _ = store.HasStub(endpointName, div)
			}

			if globalDebug {
				if ok {
					kvs := []log.Fielder{
						log.KV{K: "msg", V: "vcr request"},
						log.KV{K: "http.method", V: method},
						log.KV{K: "http.path", V: path},
						log.KV{K: "vcr.endpoint", V: endpointName},
					}
					if div != "" {
						kvs = append(kvs, log.KV{K: "vcr.var", V: div})
					}
					if rawQuery != "" {
						kvs = append(kvs, log.KV{K: "http.query", V: rawQuery})
					}
					if streamType != "" {
						kvs = append(kvs, log.KV{K: "http.stream", V: streamType})
					}
					if loopback {
						kvs = append(kvs, log.KV{K: "vcr.loopback", V: true})
					}
					log.Debug(ctx, kvs...)
				} else {
					kvs := []log.Fielder{
						log.KV{K: "msg", V: "vcr request"},
						log.KV{K: "http.method", V: method},
						log.KV{K: "http.path", V: path},
						log.KV{K: "vcr.endpoint", V: "<unmatched>"},
					}
					if rawQuery != "" {
						kvs = append(kvs, log.KV{K: "http.query", V: rawQuery})
					}
					if streamType != "" {
						kvs = append(kvs, log.KV{K: "http.stream", V: streamType})
					}
					if loopback {
						kvs = append(kvs, log.KV{K: "vcr.loopback", V: true})
					}
					log.Debug(ctx, kvs...)
				}
			}

			next.ServeHTTP(rc, r)

			// Emit an always-on warning when a unary request ends up unstubbed.
			if ok && streamType == "" && !hasStub && rc.status == http.StatusNotImplemented {
				kvs := []log.Fielder{
					log.KV{K: "msg", V: "vcr unstubbed unary request (no stub on disk)"},
					log.KV{K: "vcr.unstubbed", V: true},
					log.KV{K: "vcr.endpoint", V: endpointName},
					log.KV{K: "http.method", V: method},
					log.KV{K: "http.path", V: path},
					log.KV{K: "hint", V: "record this endpoint or adjust vcr.json variant settings"},
				}
				if div != "" {
					kvs = append(kvs, log.KV{K: "vcr.var", V: div})
				}
				if rawQuery != "" {
					kvs = append(kvs, log.KV{K: "http.query", V: rawQuery})
				}
				log.Warn(ctx, kvs...)
			}

			if globalDebug {
				log.Debug(ctx,
					log.KV{K: "msg", V: "vcr response"},
					log.KV{K: "http.method", V: method},
					log.KV{K: "http.path", V: path},
					log.KV{K: "http.status", V: rc.status},
					log.KV{K: "http.bytes", V: rc.bytes},
					log.KV{K: "http.time_ms", V: time.Since(start).Milliseconds()},
				)
			}
		})
	}
}

type stringFlag struct {
	value string
	set   bool
}

func (f *stringFlag) String() string { return f.value }

func (f *stringFlag) Set(value string) error {
	f.value = value
	f.set = true
	return nil
}

func cmdRecord(args []string, cfg CLIConfig) int {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	portFlag := fs.Int("port", cfg.DefaultPort, "Port to listen on")
	var upstreamFlag stringFlag
	upstreamFlag.value = cfg.DefaultUpstream
	fs.Var(&upstreamFlag, "upstream", "Upstream base URL when creating a policy")
	maxVariantsFlag := fs.Int("max-variants", cfg.DefaultMaxVariants, "Max distinct query variants per endpoint before auto-ignoring query (heuristic)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr,
			"Usage: %s record [options] <testdata-dir>\n\n"+
				"Start a recording proxy that captures upstream responses as VCR stubs.\n\n"+
				"If vcr.json is missing, it will be created using -upstream.\n\n"+
				"If vcr.json sets endpoints.<EndpointName>.variant.query=false, then query strings\n"+
				"are ignored for that endpoint (stubs will be undiversified).\n\n"+
				"Heuristic: if an endpoint records more than -max-variants distinct query variants\n"+
				"in one session and policy does not explicitly set variant.query, the recorder will:\n"+
				"  - persist endpoints.<EndpointName>.variant.query=false\n"+
				"  - delete existing stubs for that endpoint\n"+
				"  - wait for the next call to record the undiversified stub\n\n"+
				"Options:\n",
			cfg.AppName,
		)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr,
			"Examples:\n"+
				"  # Start recording proxy (forwards auth headers from incoming requests)\n"+
				"  %[1]s record ./testdata\n\n"+
				"  # Configure your backend to use http://localhost:%[2]d as the upstream URL\n"+
				"  # Exercise the flows you want to capture\n"+
				"  # Press Ctrl+C when done\n",
			cfg.AppName,
			cfg.DefaultPort,
		)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 1
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 1
	}

	outDir := fs.Arg(0)

	ctx := cmdContext(cfg.AppName)
	ctx = log.With(ctx, log.KV{K: "subcmd", V: "record"})

	if err := ensurePolicy(outDir, upstreamFlag.value, upstreamFlag.set); err != nil {
		log.Errorf(ctx, err, "failed to ensure policy")
		return 1
	}

	store, err := vcrruntime.New(outDir)
	if err != nil {
		log.Errorf(ctx, err, "failed to load policy")
		return 1
	}
	if store.Policy.Upstream == "" {
		log.Errorf(ctx, fmt.Errorf("%s must exist and define an upstream", vcrruntime.PolicyFileName), "invalid policy")
		return 1
	}

	endpoints := Endpoints()
	if len(endpoints) == 0 {
		log.Errorf(ctx, fmt.Errorf("no mount points found"), "invalid mount points")
		return 1
	}

	upstreamURL, err := url.Parse(store.Policy.Upstream)
	if err != nil {
		log.Errorf(ctx, err, "invalid upstream URL")
		return 1
	}

	proxy := httputil.NewSingleHostReverseProxy(upstreamURL)
	proxy.Transport = vcrruntime.NewRecordingTransport(ctx, store, endpoints, proxy.Transport, *maxVariantsFlag)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = upstreamURL.Host
		// Prefer uncompressed responses for stable recordings.
		if req.Method == http.MethodGet {
			req.Header.Del("Accept-Encoding")
		}
	}

	addr := fmt.Sprintf(":%d", *portFlag)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           proxy,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Print(ctx, log.KV{K: "msg", V: "shutting down"})
		cancel()
		_ = httpServer.Close()
	}()

	log.Print(ctx, log.KV{K: "http-addr", V: addr}, log.KV{K: "vcr.upstream", V: store.Policy.Upstream})

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf(ctx, err, "server error")
		return 1
	}
	return 0
}

// cmdPlay implements the "play" subcommand.
func cmdPlay(args []string, cfg CLIConfig) int {
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	portFlag := fs.Int("port", cfg.DefaultPort, "Port to listen on")
	scenarioFlag := fs.String("scenario", cfg.DefaultScenario, "Scenario name (streaming + background-override endpoints)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr,
			"Usage: %s play [options] <background-dir>\n\n"+
				"Serve recorded VCR stubs as an HTTP API using Goa-generated server code and\n"+
				"goa-vcr generated glue.\n\n"+
				"Streaming endpoints (WebSocket/SSE) require a scenario handler; unary endpoints\n"+
				"fall back to stubbed background behavior when no scenario handler is set.\n\n"+
				"Options:\n",
			cfg.AppName,
		)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr,
			"Examples:\n"+
				"  %[1]s play ./testdata\n",
			cfg.AppName,
		)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 1
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 1
	}

	outDir := fs.Arg(0)

	ctx := cmdContext(cfg.AppName)
	ctx = log.With(ctx, log.KV{K: "subcmd", V: "play"})

	store, err := vcrruntime.New(outDir)
	if err != nil {
		log.Errorf(ctx, err, "failed to load policy")
		return 1
	}
	if store.Policy.Upstream == "" {
		log.Errorf(ctx, fmt.Errorf("%s must exist and define an upstream", vcrruntime.PolicyFileName), "invalid policy")
		return 1
	}

	addr := fmt.Sprintf("127.0.0.1:%d", *portFlag)
	baseURL := fmt.Sprintf("http://%s", addr)

	factory, ok := cfg.ScenarioRegistry[*scenarioFlag]
	if !ok {
		log.Errorf(ctx, fmt.Errorf("unknown scenario %q", *scenarioFlag), "invalid scenario")
		return 1
	}

	loopbackDoer := vcrruntime.NewStubDoer(store, Endpoints())
	sc, _, err := BuildScenario(baseURL, loopbackDoer, factory)
	if err != nil {
		log.Errorf(ctx, err, "failed to build scenario")
		return 1
	}

	h, err := NewPlaybackHandler(store, sc, PlaybackOptions{ScenarioName: *scenarioFlag})
	if err != nil {
		log.Errorf(ctx, err, "failed to build playback handler")
		return 1
	}
	// Order matters: install a clue/log logger in the request context first,
	// then run the debug access log middleware.
	h = vcrAccessLog(store)(h)
	h = withRequestLogContext(h)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Print(ctx, log.KV{K: "msg", V: "shutting down"})
		cancel()
		_ = httpServer.Close()
	}()

	log.Print(ctx, log.KV{K: "http-addr", V: addr}, log.KV{K: "vcr.scenario", V: *scenarioFlag})

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf(ctx, err, "server error")
		return 1
	}
	return 0
}

const defaultContentType = "application/json"

// cmdRefresh implements the "refresh" subcommand.
func cmdRefresh(args []string, cfg CLIConfig) int {
	fs := flag.NewFlagSet("refresh", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	tokenFlag := fs.String("token", "", "Bearer token for authentication")
	dryRunFlag := fs.Bool("dry-run", false, "Parse files but don't make HTTP requests")
	verboseFlag := fs.Bool("v", false, "Verbose output")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr,
			"Usage: %s refresh [options] <testdata-dir>\n\n"+
				"Refresh VCR stubs by re-fetching from upstream endpoints.\n\n"+
				"For each .vcr.har file found in the testdata directory, this command:\n"+
				"  1. Parses HAR request metadata to extract URL, method\n"+
				"  2. Validates all files in a directory target the same hostname\n"+
				"  3. Makes HTTP requests with the provided auth token\n"+
				"  4. Writes responses to corresponding .vcr.json files and updates HAR metadata\n\n"+
				"Options:\n",
			cfg.AppName,
		)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr,
			"Examples:\n"+
				"  %[1]s refresh -token=\"$TOKEN\" ./testdata\n"+
				"  %[1]s refresh -dry-run ./testdata\n",
			cfg.AppName,
		)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 1
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return 1
	}
	if *tokenFlag == "" && !*dryRunFlag {
		fmt.Fprintln(os.Stderr, "error: -token is required (or use -dry-run)")
		return 1
	}

	dir := fs.Arg(0)
	if err := refreshDir(dir, *tokenFlag, *dryRunFlag, *verboseFlag); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func ensurePolicy(dir, upstream string, upstreamSet bool) error {
	path := filepath.Join(dir, vcrruntime.PolicyFileName)
	if _, err := os.Stat(path); err == nil {
		if !upstreamSet {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		var policy vcrruntime.Policy
		if err := json.Unmarshal(data, &policy); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if policy.Upstream != "" && policy.Upstream != upstream {
			return fmt.Errorf("upstream mismatch: flag=%q policy=%q", upstream, policy.Upstream)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	policy := vcrruntime.Policy{Upstream: upstream}
	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func refreshDir(dir, token string, dryRun, verbose bool) error {
	store, err := vcrruntime.New(dir)
	if err != nil {
		return err
	}

	files, err := scanDir(dir)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "%s: no .vcr.har files found\n", dir)
		return nil
	}

	if err := validateHosts(store, files, verbose); err != nil {
		return err
	}

	matcher := vcrruntime.NewRouteMatcher(Endpoints())
	for _, name := range files {
		if err := executeVCR(store, matcher, name, token, dryRun, verbose); err != nil {
			fmt.Fprintf(os.Stderr, "%s.vcr.har: %v\n", name, err)
		}
	}
	return nil
}

func scanDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".vcr.har") || entry.Name() == vcrruntime.PolicyFileName {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".vcr.har")
		files = append(files, name)
	}
	return files, nil
}

func validateHosts(store *vcrruntime.VCR, files []string, verbose bool) error {
	if len(files) == 0 {
		return nil
	}

	var expectedHost string
	if store.Policy.Upstream != "" {
		expectedHost = store.Policy.Host()
	} else {
		endpointName, diversifier := splitStubKey(files[0])
		firstReq, err := store.ReadRequest(endpointName, diversifier)
		if err != nil {
			return fmt.Errorf("read %s: %w", files[0], err)
		}
		expectedHost = firstReq.Host
	}

	var mismatches []string
	for _, name := range files {
		endpointName, diversifier := splitStubKey(name)
		req, err := store.ReadRequest(endpointName, diversifier)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if req.Host != expectedHost {
			mismatches = append(mismatches, fmt.Sprintf("  %s.vcr.har: %s", name, req.Host))
		}
	}

	if len(mismatches) > 0 {
		source := "first file"
		if store.Policy.Upstream != "" {
			source = vcrruntime.PolicyFileName
		}
		return fmt.Errorf("hostname mismatch in %s:\n  expected: %s (from %s)\n%s",
			store.Root, expectedHost, source, strings.Join(mismatches, "\n"))
	}

	if verbose {
		fmt.Printf("%s: all %d files target %s\n", store.Root, len(files), expectedHost)
	}
	return nil
}

func executeVCR(store *vcrruntime.VCR, matcher *vcrruntime.RouteMatcher, name string, token string, dryRun, verbose bool) error {
	endpointName, diversifier := splitStubKey(name)
	reqSpec, err := store.ReadRequest(endpointName, diversifier)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if verbose || dryRun {
		fmt.Printf("%s: GET %s\n", name, reqSpec.URL)
	}
	if dryRun {
		return nil
	}

	body, status, headers, err := doRequest(&reqSpec, token)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	if !json.Valid(body) {
		return fmt.Errorf("response is not JSON")
	}

	blobBytes := body
	var buf bytes.Buffer
	if err := json.Indent(&buf, body, "", "  "); err == nil {
		buf.WriteByte('\n')
		blobBytes = buf.Bytes()
	}

	mimeType := headers.Get("Content-Type")
	if mimeType == "" {
		mimeType = defaultContentType
	}

	// Recompute diversifier from the request URL using the route matcher and policy.
	newDiv, err := requestDiversifierForURL(store, matcher, reqSpec.URL)
	if err != nil {
		return fmt.Errorf("compute diversifier: %w", err)
	}

	if err := store.WriteStub(endpointName, vcrruntime.RequestSpec{URL: reqSpec.URL}, vcrruntime.ResponseMeta{
		Status:   status,
		Headers:  firstHeaderValues(headers),
		MimeType: mimeType,
		Size:     len(blobBytes),
	}, blobBytes, newDiv); err != nil {
		return err
	}

	fmt.Printf("%s: wrote %d bytes to %s\n", name, len(blobBytes), stubKey(endpointName, newDiv)+".vcr.json")
	return nil
}

func doRequest(req *vcrruntime.RequestSpec, token string) ([]byte, int, http.Header, error) {
	httpReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, req.URL, nil)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("create request: %w", err)
	}

	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}
	if httpReq.Header.Get("Accept") == "" {
		httpReq.Header.Set("Accept", "application/json")
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, 0, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, resp.StatusCode, resp.Header, nil
}

func requestDiversifierForURL(store *vcrruntime.VCR, matcher *vcrruntime.RouteMatcher, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	r, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	endpointName, vars, ok := matcher.Match(r)
	if !ok {
		// If we can't match, fall back to query-only diversifier (best effort).
		return vcrruntime.QueryDiversifier(u.Query()), nil
	}
	return vcrruntime.RequestDiversifier(store.Policy, endpointName, u.Query(), vars), nil
}

func splitStubKey(name string) (string, string) {
	parts := strings.SplitN(name, "--", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return name, ""
}

func stubKey(endpointName, diversifier string) string {
	if diversifier == "" {
		return endpointName
	}
	return endpointName + "--" + diversifier
}

func firstHeaderValues(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for name, values := range headers {
		if len(values) == 0 {
			continue
		}
		out[name] = values[0]
	}
	return out
}
`
