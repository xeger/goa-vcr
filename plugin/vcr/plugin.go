package vcr

import (
	"sort"

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
	var root *expr.RootExpr
	for _, r := range roots {
		if rr, ok := r.(*expr.RootExpr); ok {
			root = rr
			break
		}
	}
	if root == nil || root.API == nil || root.API.HTTP == nil {
		return files, nil
	}

	services := httpcodegen.NewServicesData(service.NewServicesData(root), root.API.HTTP)

	// Deterministic service ordering.
	names := make([]string, 0, len(root.Services))
	for _, s := range root.Services {
		names = append(names, s.Name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc := services.Get(name) // lazily computes services.HTTPData[name]
		if svc == nil || svc.Service == nil {
			continue
		}
		spec := vcrgen.BuildServiceSpec(genpkg, svc)
		files = append(files, vcrgen.RenderServiceVCR(spec))
	}
	return files, nil
}

