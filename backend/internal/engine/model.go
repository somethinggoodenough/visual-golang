package engine

// These structs implement the JSON contract in docs/design.md. Statement and
// expression union decoding is strict; semantic checks run before generation.
type Program struct {
	SchemaVersion string `json:"schemaVersion"`
	ID string `json:"id"`
	PackageName string `json:"packageName"`
	EntryFunctionID string `json:"entryFunctionId"`
	Functions []Function `json:"functions"`
	Symbols []Symbol `json:"symbols"`
}
type Function struct {
	ID string `json:"id"`
	Kind string `json:"kind"`
	ParentFunctionID *string `json:"parentFunctionId"`
	Body Block `json:"body"`
}
type Block struct {
	ID string `json:"id"`
	Statements []Statement `json:"statements"`
}
type Symbol struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	ScopeID string `json:"scopeId"`
}
type Expr struct {
	ID string `json:"id"`
	Kind string `json:"kind"`
	Value any `json:"value,omitempty"` // int literals are decimal strings; bool literals are booleans.
	SymbolID string `json:"symbolId,omitempty"`
}
type Statement struct {
	ID string `json:"id"`
	Kind string `json:"kind"`
	SymbolID string `json:"symbolId,omitempty"`
	Capacity int `json:"capacity,omitempty"`
	Value *Expr `json:"value,omitempty"`
	FunctionID string `json:"functionId,omitempty"`
	ChannelSymbolID string `json:"channelSymbolId,omitempty"`
	Bindings []*string `json:"bindings,omitempty"`
}
type Span struct {
	File string `json:"file"`
	StartByte int `json:"startByte"`
	EndByte int `json:"endByte"`
	StartLine int `json:"startLine"`
	StartColumn int `json:"startColumn"`
}
type Diagnostic struct {
	Code string `json:"code"`
	Severity string `json:"severity"`
	Phase string `json:"phase"`
	Message string `json:"message"`
	NodeID string `json:"nodeId,omitempty"`
	Span *Span `json:"span,omitempty"`
}
type Result struct {
	Status string `json:"status"`
	IR *Program `json:"ir,omitempty"`
	Source string `json:"source,omitempty"`
	SourceMap map[string]Span `json:"sourceMap,omitempty"`
	SourceHash string `json:"sourceHash,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Configured once at service startup. These are product limits, not Go limits.
var MaxNodes = 500
var MaxCapacity = 1024
const MaxSourceBytes = 1 << 20

func StringPtr(s string) *string { return &s }
