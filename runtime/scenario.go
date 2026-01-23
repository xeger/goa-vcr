package runtime

import "goa.design/clue/mock"

// Scenario is a name-keyed queue of handlers, backed by clue/mock.Mock.
//
// Generated code typically provides typed wrapper methods (Set*/Add*) around
// these primitives.
type Scenario struct {
	m *mock.Mock
}

// NewScenario returns a scenario backed by clue/mock.
func NewScenario() Scenario {
	return Scenario{m: mock.New()}
}

func (s *Scenario) ensureMock() *mock.Mock {
	if s.m == nil {
		s.m = mock.New()
	}
	return s.m
}

// Next returns the next handler for the named endpoint, if any.
func (s Scenario) Next(name string) any {
	if s.m == nil {
		return nil
	}
	return s.m.Next(name)
}

// Set sets the handler for name, overwriting any existing handler.
func (s *Scenario) Set(name string, handler any) {
	s.ensureMock().Set(name, handler)
}

// Add appends handler to the queue for name.
func (s *Scenario) Add(name string, handler any) {
	s.ensureMock().Add(name, handler)
}

