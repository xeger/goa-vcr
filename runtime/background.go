package runtime

import (
	"context"
	"fmt"

	"goa.design/clue/log"
)

// NoScenarioHandler logs and returns an error indicating that a service method
// reached the recorded-stub background but cannot be served from recordings:
// the recorder only captures GET 200 JSON responses, so mutations (POST / PUT
// / PATCH / DELETE) and other non-GET methods must be handled by a scenario
// layer. The caller forgot to register one.
//
// The log entry uses sentinel vcr.action="playback_no_scenario_handler" so it
// can be distinguished from playback_stub_miss (a missing recording for a
// recordable method) and playback_route_miss (an unrecognized request path).
func NoScenarioHandler(ctx context.Context, method, verb string) error {
	err := fmt.Errorf(
		"vcr: no scenario handler for %s (%s is not recorded; register a handler via Scenario.Set%s or Scenario.Add%s)",
		method, verb, method, method,
	)
	log.Error(ctx, err,
		log.KV{K: "vcr.action", V: "playback_no_scenario_handler"},
		log.KV{K: "vcr.endpoint.name", V: method},
		log.KV{K: "http.method", V: verb},
	)
	return err
}

// RecordNoScenarioHandler logs and returns an error when record mode reaches a
// request that cannot be recorded and was not handled by a scenario layer.
func RecordNoScenarioHandler(ctx context.Context, method, verb string) error {
	err := fmt.Errorf(
		"vcr: no scenario handler for %s (%s is not recorded; register a handler via Scenario.Set%s or Scenario.Add%s)",
		method, verb, method, method,
	)
	log.Error(ctx, err,
		log.KV{K: "vcr.action", V: "record_no_scenario_handler"},
		log.KV{K: "vcr.endpoint.name", V: method},
		log.KV{K: "http.method", V: verb},
	)
	return err
}
