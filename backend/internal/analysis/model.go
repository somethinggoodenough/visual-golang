// Package analysis projects single-file Go into a read-only, source-aware
// concurrency structure. It is separate from the editable v0 engine IR and
// contains no execution state, runtime channel identities, or layout.
package analysis

import "goviz/backend/internal/engine"

type Result struct {
	Status      string              `json:"status"`
	SourceHash  string              `json:"sourceHash"`
	Model       *Program            `json:"model,omitempty"`
	Diagnostics []engine.Diagnostic `json:"diagnostics"`
}

type Program struct {
	SchemaVersion string     `json:"schemaVersion"`
	PackageName   string     `json:"packageName"`
	Functions     []Function `json:"functions"`
	Nodes         []Node     `json:"nodes"`
	Channels      []Channel  `json:"channels"`
}

type Function struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Receiver         string      `json:"receiver,omitempty"`
	ParentFunctionID string      `json:"parentFunctionId,omitempty"`
	Span             engine.Span `json:"span"`
}

// ParentID describes lexical nesting, not execution or happens-before order.
// IDs are reproducible within one source snapshot; use SourceHash to reject
// mappings from a different snapshot.
type Node struct {
	ID               string      `json:"id"`
	Kind             string      `json:"kind"`
	Label            string      `json:"label"`
	ParentID         string      `json:"parentId,omitempty"`
	FunctionID       string      `json:"functionId,omitempty"`
	ChannelID        string      `json:"channelId,omitempty"`
	TargetFunctionID string      `json:"targetFunctionId,omitempty"`
	Opaque           bool        `json:"opaque,omitempty"`
	Span             engine.Span `json:"span"`
}

// Channel describes a declared channel variable/field, not a runtime channel.
// Distinct declarations may alias one channel; one field may hold many channel
// instances. An unresolved expression intentionally has no ChannelID.
type Channel struct {
	ID   string      `json:"id"`
	Name string      `json:"name"`
	Type string      `json:"type"`
	Kind string      `json:"kind"`
	Span engine.Span `json:"span"`
}
