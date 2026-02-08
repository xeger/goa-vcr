package vcrgen

import (
	"strings"

	httpcodegen "goa.design/goa/v3/http/codegen"
)

func BuildServiceSpec(genpkg string, svc *httpcodegen.ServiceData) ServiceSpec {
	spec := ServiceSpec{
		GenPkg:          genpkg,
		ServicePathName: svc.Service.PathName,
		ServicePkgName:  svc.Service.PkgName,
		HasWebSocket:    httpcodegen.HasWebSocket(svc),
	}

	for _, ed := range svc.Endpoints {
		// Prefer HTTP codegen refs when available (they're already qualified for
		// use outside the service package).
		payloadRef := qualifyTypeRef(spec.ServicePkgName, ed.Method.PayloadRef)
		if ed.Payload != nil && ed.Payload.Ref != "" {
			payloadRef = ed.Payload.Ref
		}
		resultRef := qualifyTypeRef(spec.ServicePkgName, ed.Method.ResultRef)
		if ed.Result != nil && ed.Result.Ref != "" {
			resultRef = ed.Result.Ref
		}

		var viewedInitName string
		var viewedViewName string
		if ed.Method.ViewedResult != nil && ed.Method.ViewedResult.Init != nil {
			viewedInitName = ed.Method.ViewedResult.Init.Name
			viewedViewName = ed.Method.ViewedResult.ViewName
			spec.HasViewedResult = true
		}

		ep := EndpointSpec{
			MethodName:    ed.Method.Name,
			MethodVarName: ed.Method.VarName,
			PayloadRef:    payloadRef,
			ResultRef:     resultRef,
			IsStreaming:   httpcodegen.IsWebSocketEndpoint(ed) || httpcodegen.IsSSEEndpoint(ed),
			ViewedResultInitName: viewedInitName,
			ViewedResultViewName: viewedViewName,
		}
		for _, r := range ed.Routes {
			if r.Verb == "OPTIONS" {
				continue // Skip CORS preflight mounts.
			}
			ep.Routes = append(ep.Routes, RouteSpec{Verb: r.Verb, Path: r.Path})
		}
		spec.Endpoints = append(spec.Endpoints, ep)
	}
	return spec
}

func qualifyTypeRef(pkgName, ref string) string {
	if ref == "" || pkgName == "" {
		return ref
	}
	// Already qualified (or includes a pkg override).
	if strings.Contains(ref, ".") {
		return ref
	}
	// Common Goa method refs: "*Payload", "*Result", "[]*Result", "[]Result".
	switch {
	case strings.HasPrefix(ref, "[]*"):
		return "[]*" + pkgName + "." + strings.TrimPrefix(ref, "[]*")
	case strings.HasPrefix(ref, "[]"):
		return "[]" + pkgName + "." + strings.TrimPrefix(ref, "[]")
	case strings.HasPrefix(ref, "*"):
		return "*" + pkgName + "." + strings.TrimPrefix(ref, "*")
	default:
		return pkgName + "." + ref
	}
}
