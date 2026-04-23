package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
)

// ScenarioSpec names one scenario layer in a stack.
type ScenarioSpec struct {
	Name string `json:"name"`
}

// ActiveScenarios manages the mutable active scenario stack and delegates
// playback requests to the currently configured handler.
type ActiveScenarios struct {
	build func([]ScenarioSpec) (http.Handler, error)

	mu             sync.RWMutex
	registeredSet  map[string]struct{}
	registeredList []ScenarioSpec
	defaults       []ScenarioSpec
	active         []ScenarioSpec

	playback atomic.Value // stores http.Handler
}

type scenariosState struct {
	Registered []ScenarioSpec `json:"registered"`
	Default    []ScenarioSpec `json:"default"`
	Active     []ScenarioSpec `json:"active"`
}

// NewActiveScenarios validates defaults, builds the initial playback handler,
// and returns an active scenario controller.
func NewActiveScenarios(
	registered []string,
	defaults []ScenarioSpec,
	build func([]ScenarioSpec) (http.Handler, error),
) (*ActiveScenarios, error) {
	if build == nil {
		return nil, errors.New("active scenarios: nil builder")
	}

	regSet := make(map[string]struct{}, len(registered))
	regList := make([]ScenarioSpec, 0, len(registered))
	for _, name := range registered {
		if name == "" {
			return nil, errors.New("active scenarios: registered scenario name cannot be empty")
		}
		if _, seen := regSet[name]; seen {
			continue
		}
		regSet[name] = struct{}{}
		regList = append(regList, ScenarioSpec{Name: name})
	}
	if err := validateScenarioSpecs(defaults, regSet); err != nil {
		return nil, err
	}

	handler, err := build(defaults)
	if err != nil {
		return nil, fmt.Errorf("active scenarios: build defaults: %w", err)
	}
	if handler == nil {
		return nil, errors.New("active scenarios: builder returned nil handler")
	}

	a := &ActiveScenarios{
		build:          build,
		registeredSet:  regSet,
		registeredList: regList,
		defaults:       cloneScenarioSpecs(defaults),
		active:         cloneScenarioSpecs(defaults),
	}
	a.playback.Store(handler)
	return a, nil
}

// ServeHTTP forwards playback requests to the current active handler.
func (a *ActiveScenarios) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler, _ := a.playback.Load().(http.Handler)
	if handler == nil {
		http.Error(w, `{"error":"vcr: active playback handler unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, r)
}

// HandleScenarios serves GET /__vcr__/scenarios.
func (a *ActiveScenarios) HandleScenarios(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	state := a.snapshot()
	writeJSON(w, http.StatusOK, state)
}

// HandleActiveScenarios serves PUT and DELETE /__vcr__/scenarios/active.
func (a *ActiveScenarios) HandleActiveScenarios(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPut:
		specs, err := decodeScenarioSpecs(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		if err := a.ReplaceActive(specs); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, a.snapshot())
	case http.MethodDelete:
		if err := a.RestoreDefaults(); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, a.snapshot())
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// ReplaceActive validates specs, builds a replacement playback handler, then
// atomically swaps the active stack and handler.
func (a *ActiveScenarios) ReplaceActive(specs []ScenarioSpec) error {
	a.mu.RLock()
	reg := a.registeredSet
	a.mu.RUnlock()

	if err := validateScenarioSpecs(specs, reg); err != nil {
		return err
	}
	handler, err := a.build(specs)
	if err != nil {
		return fmt.Errorf("active scenarios: build active scenarios: %w", err)
	}
	if handler == nil {
		return errors.New("active scenarios: builder returned nil handler")
	}

	a.mu.Lock()
	a.active = cloneScenarioSpecs(specs)
	a.mu.Unlock()
	a.playback.Store(handler)
	return nil
}

// RestoreDefaults resets the active scenario list and playback handler to the
// startup defaults.
func (a *ActiveScenarios) RestoreDefaults() error {
	a.mu.RLock()
	defaults := cloneScenarioSpecs(a.defaults)
	a.mu.RUnlock()
	return a.ReplaceActive(defaults)
}

func (a *ActiveScenarios) snapshot() scenariosState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return scenariosState{
		Registered: cloneScenarioSpecs(a.registeredList),
		Default:    cloneScenarioSpecs(a.defaults),
		Active:     cloneScenarioSpecs(a.active),
	}
}

func validateScenarioSpecs(specs []ScenarioSpec, registered map[string]struct{}) error {
	for i := range specs {
		name := specs[i].Name
		if name == "" {
			return fmt.Errorf("active scenarios: scenarios[%d].name cannot be empty", i)
		}
		if _, ok := registered[name]; !ok {
			known := make([]string, 0, len(registered))
			for n := range registered {
				known = append(known, n)
			}
			slices.Sort(known)
			return fmt.Errorf("active scenarios: unknown scenario %q (known: %v)", name, known)
		}
	}
	return nil
}

func decodeScenarioSpecs(body io.Reader) ([]ScenarioSpec, error) {
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var specs []ScenarioSpec
	if err := dec.Decode(&specs); err != nil {
		return nil, fmt.Errorf("active scenarios: invalid request body: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("active scenarios: request body must contain exactly one JSON array")
	}
	return specs, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func cloneScenarioSpecs(in []ScenarioSpec) []ScenarioSpec {
	out := make([]ScenarioSpec, len(in))
	copy(out, in)
	return out
}
