package vcr

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/codegen/service"
	"goa.design/goa/v3/eval"
	"goa.design/goa/v3/expr"
	httpcodegen "goa.design/goa/v3/http/codegen"

	"github.com/xeger/goa-vcr/plugin/vcr/internal/vcrgen"
)

func init() {
	codegen.RegisterPlugin("vcr", "gen", nil, Generate)
}

// Generate is invoked by `goa gen` and may append additional generated files.
func Generate(genpkg string, roots []eval.Root, files []*codegen.File) ([]*codegen.File, error) {
	if os.Getenv("GOA_VCR_DEBUG") != "" {
		var b strings.Builder
		_, _ = fmt.Fprintf(&b, "goa-vcr plugin: genpkg=%s\n", genpkg)
		_, _ = fmt.Fprintf(&b, "roots=%d\n", len(roots))
		for i, r := range roots {
			if rr, ok := r.(*expr.RootExpr); ok {
				hasHTTP := rr.API != nil && rr.API.HTTP != nil
				_, _ = fmt.Fprintf(&b, "%d: root=%s services=%d hasHTTP=%v\n", i, rr.EvalName(), len(rr.Services), hasHTTP)
				for _, s := range rr.Services {
					_, _ = fmt.Fprintf(&b, "  - %s\n", s.Name)
				}
			} else {
				_, _ = fmt.Fprintf(&b, "%d: root=%T\n", i, r)
			}
		}
		files = append(files, &codegen.File{
			Path: filepath.Join(codegen.Gendir, "vcr_debug.txt"),
			SectionTemplates: []*codegen.SectionTemplate{
				{Name: "debug", Source: b.String()},
			},
		})
	}

	// Goa may provide multiple roots (e.g. when the design imports subpackages).
	// Generate VCR glue for every HTTP service in every Goa root, mirroring how
	// Goa iterates roots in its built-in generators.
	seen := make(map[string]struct{})
	for _, r := range roots {
		root, ok := r.(*expr.RootExpr)
		if !ok || root.API == nil {
			continue
		}

		services := httpcodegen.NewServicesData(service.NewServicesData(root), root.API.HTTP)

		// Deterministic service ordering per root.
		names := make([]string, 0, len(root.Services))
		for _, s := range root.Services {
			names = append(names, s.Name)
		}
		sort.Strings(names)

		for _, name := range names {
			if _, ok := seen[name]; ok {
				continue
			}
			svc := services.Get(name) // lazily computes services.HTTPData[name]
			if svc == nil || svc.Service == nil {
				continue
			}
			spec := vcrgen.BuildServiceSpec(genpkg, svc)
			f := vcrgen.RenderServiceVCR(spec)
			cli := vcrgen.RenderServiceVCRCLI(spec)
			// Ensure we import any extra packages required by the service types.
			// This includes:
			// - user types generated into separate packages via "struct:pkg:path"
			// - meta types referenced via "struct:field:type" (e.g. types.UUID)
			if len(f.SectionTemplates) > 0 {
				svcExpr := root.Service(name)
				if svcExpr != nil {
					service.AddServiceDataMetaTypeImports(f.SectionTemplates[0], svcExpr, svc.Service)
				}
				service.AddUserTypeImports(genpkg, f.SectionTemplates[0], svc.Service)
			}
			files = append(files, f, cli)
			seen[name] = struct{}{}
		}
	}
	return files, nil
}
