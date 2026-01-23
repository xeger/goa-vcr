package vcrgen

type ServiceSpec struct {
	GenPkg          string
	ServicePathName string
	ServicePkgName  string
	HasWebSocket    bool
	Endpoints       []EndpointSpec
}

type EndpointSpec struct {
	MethodVarName string
	PayloadRef    string
	ResultRef     string
	IsStreaming   bool
	Routes        []RouteSpec
}

type RouteSpec struct {
	Verb string
	Path string
}

