package design

import . "goa.design/goa/v3/dsl"

var Thing = Type("Thing", func() {
	Attribute("id", String, "Thing identifier")
	Required("id")
})

var ThingWithViews = ResultType("ThingWithViews", func() {
	Description("A result type with multiple views to exercise Goa viewed results.")
	Attribute("id", String, "Thing identifier")
	Attribute("name", String, "Thing name")
	Attribute("secret", String, "Only present in the extended view")
	Required("id", "name")

	View("default", func() {
		Attribute("id")
		Attribute("name")
	})

	View("extended", func() {
		Attribute("id")
		Attribute("name")
		Attribute("secret")
	})
})

var ThingEvent = Type("ThingEvent", func() {
	// Put this type in its own generated package to ensure our VCR plugin
	// correctly imports user types declared outside the service package.
	Meta("struct:pkg:path", "types")
	Attribute("type", String, "Event type", func() {
		Default("thing")
	})
	Attribute("id", String, "Thing identifier")
	Required("type", "id")
})

var ThingSubscription = Type("ThingSubscription", func() {
	Description("Bidirectional WebSocket subscription message.")
	Attribute("msg", String, "Client message")
	Required("msg")
})

