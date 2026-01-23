package design

import . "goa.design/goa/v3/dsl"

var _ = Service("toy", func() {
	Description("Toy service with one unary and one SSE streaming endpoint.")

	Method("get_thing", func() {
		Payload(func() {
			Attribute("id", String, "Thing identifier")
			Required("id")
		})

		Result(Thing)

		HTTP(func() {
			GET("/things/{id}")
			Param("id")
			Response(StatusOK)
		})
	})

	Method("stream_things_sse", func() {
		Payload(func() {
			Attribute("id", String, "Thing identifier")
			Required("id")
		})

		StreamingResult(ThingEvent, "Server-sent events stream of things")

		HTTP(func() {
			GET("/things/{id}/stream-sse")
			Param("id")
			Response(StatusOK)
			ServerSentEvents()
		})
	})
})

