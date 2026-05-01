package runtime

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"goa.design/clue/log"
)

func TestNoScenarioHandlerLogsAndReturnsError(t *testing.T) {
	var out bytes.Buffer
	ctx := log.Context(context.Background(),
		log.WithOutput(&out),
		log.WithFormat(log.FormatJSON),
		log.WithDisableBuffering(func(context.Context) bool { return true }),
	)

	err := NoScenarioHandler(ctx, "UpdateSettings", "PATCH")
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	msg := err.Error()
	for _, want := range []string{"UpdateSettings", "PATCH", "Scenario.SetUpdateSettings"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}

	logged := out.String()
	for _, want := range []string{
		`"level":"error"`,
		`"vcr.action":"playback_no_scenario_handler"`,
		`"vcr.endpoint.name":"UpdateSettings"`,
		`"http.method":"PATCH"`,
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("log missing %q: %s", want, logged)
		}
	}
}

func TestRecordNoScenarioHandlerLogsAndReturnsError(t *testing.T) {
	var out bytes.Buffer
	ctx := log.Context(context.Background(),
		log.WithOutput(&out),
		log.WithFormat(log.FormatJSON),
		log.WithDisableBuffering(func(context.Context) bool { return true }),
	)

	err := RecordNoScenarioHandler(ctx, "UpdateSettings", "PATCH")
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	msg := err.Error()
	for _, want := range []string{"UpdateSettings", "PATCH", "Scenario.SetUpdateSettings"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}

	logged := out.String()
	for _, want := range []string{
		`"level":"error"`,
		`"vcr.action":"record_no_scenario_handler"`,
		`"vcr.endpoint.name":"UpdateSettings"`,
		`"http.method":"PATCH"`,
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("log missing %q: %s", want, logged)
		}
	}
}
