package vcrgen

type ServiceSpec struct {
	GenPkg          string
	ServicePathName string
	ServicePkgName  string
	HasWebSocket    bool
	HasViewedResult bool
	Endpoints       []EndpointSpec
}

type EndpointSpec struct {
	MethodName    string
	MethodVarName string
	PayloadRef    string
	ResultRef     string
	IsStreaming   bool
	// ViewedResultInitName is the name of the generated helper that constructs the
	// viewed result wrapper from the service result, e.g. NewViewedOrganizationCollection.
	// Empty when the method does not return a viewed result.
	ViewedResultInitName string
	// ViewedResultViewName is the fixed view name to use when the method has at most
	// one view. Empty when view selection is dynamic.
	ViewedResultViewName string
	// SkipResponseBodyEncodeDecode is true when the service method returns a raw
	// io.ReadCloser for the HTTP response body. The typed service client method
	// has extra return values in this case, so makeEndpoint must call the raw
	// endpoint field instead.
	SkipResponseBodyEncodeDecode bool
	Routes                       []RouteSpec
}

type RouteSpec struct {
	Verb string
	Path string
}
