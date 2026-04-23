package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActiveScenariosServeHTTPAndReplace(t *testing.T) {
	ctrl := mustNewActiveController(t, []string{"Happy", "Sad"}, []ScenarioSpec{{Name: "Happy"}}, nil)

	if got := serveBody(t, ctrl); got != "Happy" {
		t.Fatalf("expected default handler body Happy, got %q", got)
	}

	if err := ctrl.ReplaceActive([]ScenarioSpec{{Name: "Sad"}, {Name: "Happy"}}); err != nil {
		t.Fatalf("replace active: %v", err)
	}
	if got := serveBody(t, ctrl); got != "Sad,Happy" {
		t.Fatalf("expected replaced handler body Sad,Happy, got %q", got)
	}
}

func TestActiveScenariosRestoreDefaults(t *testing.T) {
	ctrl := mustNewActiveController(t, []string{"Happy", "Sad"}, []ScenarioSpec{{Name: "Happy"}}, nil)

	if err := ctrl.ReplaceActive([]ScenarioSpec{{Name: "Sad"}}); err != nil {
		t.Fatalf("replace active: %v", err)
	}
	if got := serveBody(t, ctrl); got != "Sad" {
		t.Fatalf("expected Sad, got %q", got)
	}

	if err := ctrl.RestoreDefaults(); err != nil {
		t.Fatalf("restore defaults: %v", err)
	}
	if got := serveBody(t, ctrl); got != "Happy" {
		t.Fatalf("expected Happy after restore, got %q", got)
	}
}

func TestActiveScenariosReplaceActiveRejectsUnknownAndKeepsCurrent(t *testing.T) {
	ctrl := mustNewActiveController(t, []string{"Happy", "Sad"}, []ScenarioSpec{{Name: "Happy"}}, nil)

	err := ctrl.ReplaceActive([]ScenarioSpec{{Name: "Nope"}})
	if err == nil {
		t.Fatalf("expected unknown scenario error")
	}
	if !strings.Contains(err.Error(), `unknown scenario "Nope"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := serveBody(t, ctrl); got != "Happy" {
		t.Fatalf("expected Happy to remain active, got %q", got)
	}
}

func TestActiveScenariosReplaceActiveBuildErrorRollsBack(t *testing.T) {
	buildErr := errors.New("boom")
	ctrl := mustNewActiveController(
		t,
		[]string{"Happy", "Broken"},
		[]ScenarioSpec{{Name: "Happy"}},
		func(specs []ScenarioSpec) error {
			for i := range specs {
				if specs[i].Name == "Broken" {
					return buildErr
				}
			}
			return nil
		},
	)

	err := ctrl.ReplaceActive([]ScenarioSpec{{Name: "Broken"}})
	if err == nil {
		t.Fatalf("expected build failure")
	}
	if !strings.Contains(err.Error(), buildErr.Error()) {
		t.Fatalf("expected build error in message, got %v", err)
	}
	if got := serveBody(t, ctrl); got != "Happy" {
		t.Fatalf("expected Happy to remain active after failed build, got %q", got)
	}
}

func TestActiveScenariosHandleScenarios(t *testing.T) {
	ctrl := mustNewActiveController(t, []string{"Happy", "Sad"}, []ScenarioSpec{{Name: "Happy"}}, nil)
	if err := ctrl.ReplaceActive([]ScenarioSpec{{Name: "Sad"}}); err != nil {
		t.Fatalf("replace active: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/__vcr__/scenarios", nil)
	rec := httptest.NewRecorder()
	ctrl.HandleScenarios(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want %d", res.StatusCode, http.StatusOK)
	}

	var got struct {
		Registered []ScenarioSpec `json:"registered"`
		Default    []ScenarioSpec `json:"default"`
		Active     []ScenarioSpec `json:"active"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(got.Registered) != 2 || got.Registered[0].Name != "Happy" || got.Registered[1].Name != "Sad" {
		t.Fatalf("unexpected registered: %+v", got.Registered)
	}
	if len(got.Default) != 1 || got.Default[0].Name != "Happy" {
		t.Fatalf("unexpected default: %+v", got.Default)
	}
	if len(got.Active) != 1 || got.Active[0].Name != "Sad" {
		t.Fatalf("unexpected active: %+v", got.Active)
	}
}

func TestActiveScenariosHandleActiveScenariosPUTAndDELETE(t *testing.T) {
	ctrl := mustNewActiveController(t, []string{"Happy", "Sad"}, []ScenarioSpec{{Name: "Happy"}}, nil)

	putReq := httptest.NewRequest(http.MethodPut, "/__vcr__/scenarios/active", bytes.NewBufferString(`[{"name":"Sad"},{"name":"Happy"}]`))
	putRec := httptest.NewRecorder()
	ctrl.HandleActiveScenarios(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put status: got %d want %d", putRec.Code, http.StatusOK)
	}
	if got := serveBody(t, ctrl); got != "Sad,Happy" {
		t.Fatalf("expected Sad,Happy after PUT, got %q", got)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/__vcr__/scenarios/active", nil)
	delRec := httptest.NewRecorder()
	ctrl.HandleActiveScenarios(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status: got %d want %d", delRec.Code, http.StatusOK)
	}
	if got := serveBody(t, ctrl); got != "Happy" {
		t.Fatalf("expected Happy after DELETE, got %q", got)
	}
}

func TestActiveScenariosHandleActiveScenariosRejectsInvalidBody(t *testing.T) {
	ctrl := mustNewActiveController(t, []string{"Happy"}, []ScenarioSpec{{Name: "Happy"}}, nil)

	req := httptest.NewRequest(http.MethodPut, "/__vcr__/scenarios/active", strings.NewReader(`{"name":"Happy"}`))
	rec := httptest.NewRecorder()
	ctrl.HandleActiveScenarios(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusBadRequest)
	}
	if got := serveBody(t, ctrl); got != "Happy" {
		t.Fatalf("expected Happy to remain active, got %q", got)
	}
}

func mustNewActiveController(
	t *testing.T,
	registered []string,
	defaults []ScenarioSpec,
	buildGuard func([]ScenarioSpec) error,
) *ActiveScenarios {
	t.Helper()
	ctrl, err := NewActiveScenarios(
		registered,
		defaults,
		func(specs []ScenarioSpec) (http.Handler, error) {
			if buildGuard != nil {
				if err := buildGuard(specs); err != nil {
					return nil, err
				}
			}
			names := make([]string, len(specs))
			for i := range specs {
				names[i] = specs[i].Name
			}
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, strings.Join(names, ","))
			}), nil
		},
	)
	if err != nil {
		t.Fatalf("new active scenarios: %v", err)
	}
	return ctrl
}

func serveBody(t *testing.T, h http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/things/123", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Body.String()
}
