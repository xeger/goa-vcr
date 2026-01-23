package design

import . "goa.design/goa/v3/dsl"

var Thing = Type("Thing", func() {
	Attribute("id", String, "Thing identifier")
	Required("id")
})

var ThingEvent = Type("ThingEvent", func() {
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

