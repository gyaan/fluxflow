package action

import (
	"fmt"
	"sync"
)

// Registry maps action type strings to their executors.
// It is safe for concurrent reads; Register should only be called at startup.
type Registry struct {
	mu        sync.RWMutex
	executors map[string]Executor
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{executors: make(map[string]Executor)}
}

// Register adds an executor. Panics on duplicate type to surface misconfiguration early.
func (r *Registry) Register(e Executor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.executors[e.Type()]; exists {
		panic(fmt.Sprintf("action registry: duplicate type %q", e.Type()))
	}
	r.executors[e.Type()] = e
}

// Get returns the executor for the given type.
func (r *Registry) Get(actionType string) (Executor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.executors[actionType]
	if !ok {
		return nil, fmt.Errorf("no executor registered for action type %q", actionType)
	}
	return e, nil
}

// Types returns all registered action type strings.
func (r *Registry) Types() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.executors))
	for k := range r.executors {
		out = append(out, k)
	}
	return out
}
