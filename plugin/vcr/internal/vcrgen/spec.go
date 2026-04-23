package vcrgen

type ServiceSpec struct {
	GenPkg          string
	ServicePathName string
	ServicePkgName  string
	HasWebSocket    bool
	HasRawResponse  bool
	// HasViewedResult indicates at least one endpoint needs dynamic view lookup
	// from payload and therefore requires viewFromPayload/reflect support.
	HasViewedResult bool
	Endpoints       []EndpointSpec
}

type EndpointSpec struct {
	MethodName     string
	MethodVarName  string
	PayloadRef     string
	ResultRef      string
	IsStreaming    bool
	HasRawResponse bool
	// ViewedResultInitName is the name of the generated helper that constructs the
	// viewed result wrapper from the service result, e.g. NewViewedOrganizationCollection.
	// Empty when the method does not return a viewed result.
	ViewedResultInitName string
	// ViewedResultViewName is the fixed view name to use when the method has at most
	// one view. Empty when view selection is dynamic.
	ViewedResultViewName string
	// ReturnsViewName indicates this endpoint's Service method signature includes a
	// dynamic view return value: (res, view string, err).
	ReturnsViewName bool
	Routes          []RouteSpec
}

type RouteSpec struct {
	Verb string
	Path string
}
