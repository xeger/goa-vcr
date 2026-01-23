package vcrgen

import httpcodegen "goa.design/goa/v3/http/codegen"

func BuildServiceSpec(genpkg string, svc *httpcodegen.ServiceData) ServiceSpec {
	spec := ServiceSpec{
		GenPkg:          genpkg,
		ServicePathName: svc.Service.PathName,
		ServicePkgName:  svc.Service.PkgName,
		HasWebSocket:    httpcodegen.HasWebSocket(svc),
	}

	for _, ed := range svc.Endpoints {
		ep := EndpointSpec{
			MethodVarName: ed.Method.VarName,
			PayloadRef:    ed.Method.PayloadRef,
			ResultRef:     ed.Method.ResultRef,
			IsStreaming:   httpcodegen.IsWebSocketEndpoint(ed) || httpcodegen.IsSSEEndpoint(ed),
		}
		for _, r := range ed.Routes {
			ep.Routes = append(ep.Routes, RouteSpec{Verb: r.Verb, Path: r.Path})
		}
		spec.Endpoints = append(spec.Endpoints, ep)
	}
	return spec
}

