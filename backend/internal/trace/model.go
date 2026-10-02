// Package trace instruments temporary builds and normalizes real Go execution
// traces. Runtime observations remain separate from the static Program IR.
package trace

type Event struct {
	ID               string `json:"id"`
	GoroutineID      string `json:"goroutineId"`
	ActorGoroutineID string `json:"actorGoroutineId,omitempty"`
	Kind             string `json:"kind"`
	TimeNS           int64  `json:"timeNs"`
	SourceLine       int    `json:"sourceLine,omitempty"`
	NodeID           string `json:"nodeId,omitempty"`
	Detail           string `json:"detail,omitempty"`
	FromState        string `json:"fromState,omitempty"`
	ToState          string `json:"toState,omitempty"`
	Instrumentation  bool   `json:"instrumentation,omitempty"`
}

type Goroutine struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	Function   string `json:"function,omitempty"`
	SourceLine int    `json:"sourceLine,omitempty"`
	NodeID     string `json:"nodeId,omitempty"`
}

type ExecutionTrace struct {
	SchemaVersion string      `json:"schemaVersion"`
	Events        []Event     `json:"events"`
	Goroutines    []Goroutine `json:"goroutines"`
	Truncated     bool        `json:"truncated"`
}
