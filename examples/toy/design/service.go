package design

import . "goa.design/goa/v3/dsl"

var _ = Service("toy", func() {
	Description("Toy service with unary + SSE + WebSocket streaming endpoints.")

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

	Method("get_thing_viewed", func() {
		Payload(func() {
			Attribute("id", String, "Thing identifier")
			Attribute("view", String, "Response view", func() {
				Enum("default", "extended")
				Default("default")
			})
			Required("id")
		})

		Result(ThingWithViews)

		HTTP(func() {
			GET("/things/{id}/viewed")
			Param("id")
			Param("view")
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

	// Bidirectional WebSocket stream (client sends ThingSubscription, server sends ThingEvent).
	Method("stream_things_ws", func() {
		Payload(func() {
			Attribute("id", String, "Thing identifier")
			Required("id")
		})

		StreamingPayload(ThingSubscription, "Client messages")
		StreamingResult(ThingEvent, "Server messages")

		HTTP(func() {
			GET("/things/{id}/stream-ws")
			Param("id")
			Response(StatusOK)
		})
	})

	// Unidirectional WebSocket stream (server -> client only).
	Method("stream_things_ws_send_only", func() {
		Payload(func() {
			Attribute("id", String, "Thing identifier")
			Required("id")
		})

		StreamingResult(ThingEvent, "Server messages")

		HTTP(func() {
			GET("/things/{id}/stream-ws-send-only")
			Param("id")
			Response(StatusOK)
		})
	})
})

