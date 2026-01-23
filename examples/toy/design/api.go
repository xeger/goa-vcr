package design

import (
	_ "github.com/xeger/goa-vcr/plugin/vcr"
	. "goa.design/goa/v3/dsl"
)

var _ = API("toy", func() {
	Title("goa-vcr toy service")
	Description("Toy Goa service used to doc-test the goa-vcr plugin output.")
	Version("0.0.1")

	Server("toy", func() {
		Host("localhost", func() {
			URI("http://localhost:0")
		})
	})
})
