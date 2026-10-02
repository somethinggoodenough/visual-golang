package runner

import (
	"fmt"
	"strings"
	"testing"

	"goviz/backend/internal/engine"
)

func TestBuildDiagnosticsMapUTF8ByteColumns(t *testing.T) {
	source := "package main\nfunc main(){ println(\"中文\", 未定义) }\n"
	offset := strings.Index(source, "未定义")
	lineStart := strings.Index(source, "func main")
	spans := map[string]engine.Span{"call": {StartByte: strings.Index(source, "println"), EndByte: offset + len("未定义)")}, "operand": {StartByte: offset, EndByte: offset + len("未定义")}}
	ds := sourceDiagnostics("go_build", fmt.Sprintf("# command-line-arguments\n.\\main.go:2:%d: undefined: 未定义", offset-lineStart+1), source, spans, false)
	if len(ds) != 1 || ds[0].Span == nil || ds[0].Span.StartByte != offset || ds[0].Span.EndByte != offset+len("未") || ds[0].NodeID != "operand" {
		t.Fatalf("incorrect unicode diagnostic: %+v", ds)
	}
}

func TestInstrumentedBuildDiagnosticsDoNotReuseShiftedColumns(t *testing.T) {
	source := "package main\nfunc main(){ missing() }\n"
	ds := sourceDiagnostics("go_build", "main.go:2:250: undefined: missing", source, map[string]engine.Span{"misleading": {StartByte: 14, EndByte: 24}}, true)
	if len(ds) != 1 || ds[0].Span == nil || ds[0].Span.StartLine != 2 || ds[0].Span.StartColumn != 1 || ds[0].Span.StartByte != len("package main\n") || ds[0].NodeID != "" {
		t.Fatalf("incorrect instrumented diagnostic: %+v", ds)
	}
}
