package analysis

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func mustAnalyze(t *testing.T, source string) *Program {
	t.Helper()
	result := Analyze(source)
	if result.Status != "ok" || result.Model == nil {
		t.Fatalf("analysis failed: %s: %+v", result.Status, result.Diagnostics)
	}
	seen := map[string]bool{}
	for _, node := range result.Model.Nodes {
		if seen[node.ID] {
			t.Fatalf("duplicate node ID: %s", node.ID)
		}
		seen[node.ID] = true
		if node.Span.StartByte < 0 || node.Span.EndByte > len(source) || node.Span.EndByte < node.Span.StartByte {
			t.Fatalf("invalid span: %+v", node)
		}
	}
	for _, node := range result.Model.Nodes {
		if node.ParentID != "" && !seen[node.ParentID] {
			t.Fatalf("missing parent: %+v", node)
		}
	}
	return result.Model
}

func TestKVStoreStructure(t *testing.T) {
	data, err := os.ReadFile("../../../examples/kvstore.go")
	if err != nil {
		t.Fatal(err)
	}
	model := mustAnalyze(t, string(data))
	kinds := map[string]int{}
	nodes := map[string]Node{}
	for _, node := range model.Nodes {
		kinds[node.Kind]++
		nodes[node.ID] = node
		if node.Kind == "go" && node.TargetFunctionID == "" {
			t.Errorf("method spawn has no function target: %+v", node)
		}
		if (node.Kind == "send" || node.Kind == "receive" || node.Kind == "make_channel" || node.Kind == "close") && node.ChannelID == "" {
			t.Errorf("channel declaration unresolved: %+v", node)
		}
	}
	for _, kind := range []string{"function", "for", "if", "select", "select_case", "switch", "switch_case", "send", "receive", "make_channel", "close", "map_read", "map_write", "assign", "field_access", "call", "continue", "return"} {
		if kinds[kind] == 0 {
			t.Errorf("missing %s", kind)
		}
	}
	if kinds["go"] != 2 {
		t.Errorf("want two goroutine spawns, got %d", kinds["go"])
	}
	for _, node := range model.Nodes {
		if node.Kind != "switch" {
			continue
		}
		ancestors := map[string]bool{}
		for parent := node.ParentID; parent != ""; parent = nodes[parent].ParentID {
			ancestors[nodes[parent].Kind] = true
		}
		if !ancestors["select_case"] || !ancestors["select"] || !ancestors["for"] {
			t.Errorf("switch lost its select/loop nesting: %+v", ancestors)
		}
	}
	if len(model.Functions) != 7 {
		t.Errorf("want 7 functions/methods, got %d", len(model.Functions))
	}
}

func TestChannelBindingUnicodeShadowingAndParameters(t *testing.T) {
	source := `package main
type Box struct { 通道 chan int }
func forward(input <-chan int, output chan<- int) { output <- <-input }
func main() {
 通道 := make(chan int, 1)
 通道 <- 1
 { 通道 := make(chan int, 1); 通道 <- 2; _ = <-通道 }
 _ = <-通道
 box := Box{通道: make(chan int, 1)}
 box.通道 <- 3
 _ = <-box.通道
 go func() { close(通道) }()
}`
	model := mustAnalyze(t, source)
	if !reflect.DeepEqual(model, mustAnalyze(t, source)) {
		t.Fatal("same source must produce stable IDs and ordering")
	}
	var outer, inner, field string
	for _, ch := range model.Channels {
		if !utf8.ValidString(source[ch.Span.StartByte:ch.Span.EndByte]) {
			t.Errorf("span splits a Unicode identifier: %+v", ch)
		}
		if ch.Name != "通道" {
			continue
		}
		switch {
		case ch.Kind == "field":
			field = ch.ID
		case outer == "":
			outer = ch.ID
		default:
			inner = ch.ID
		}
	}
	if outer == "" || inner == "" || field == "" || inner == outer {
		t.Fatalf("bindings did not separate fields/shadowed vars: %+v", model.Channels)
	}
	for _, node := range model.Nodes {
		text := source[node.Span.StartByte:node.Span.EndByte]
		var want string
		switch text {
		case "通道 <- 1":
			want = outer
		case "通道 <- 2":
			want = inner
		case "box.通道 <- 3":
			want = field
		case "close(通道)":
			want = outer
		}
		if want != "" && node.ChannelID != want {
			t.Errorf("%s: want %s, got %s", text, want, node.ChannelID)
		}
	}
	if len(model.Functions) != 3 || model.Functions[2].ParentFunctionID != model.Functions[1].ID {
		t.Errorf("anonymous function nesting lost: %+v", model.Functions)
	}
}

func TestErrorsDoNotProduceMisleadingModels(t *testing.T) {
	for _, test := range []struct{ source, status string }{
		{"package main\nfunc main( {", "parse_error"},
		{"package main\nfunc main() { ch := make(chan int); ch <- true }", "type_error"},
		{"package main\nimport \"example.invalid/module\"\nfunc main() { module.F() }", "type_error"},
	} {
		result := Analyze(test.source)
		if result.Status != test.status || result.Model != nil || len(result.Diagnostics) == 0 || result.SourceHash == "" {
			t.Errorf("unexpected result: %+v", result)
		}
		for _, d := range result.Diagnostics {
			if d.Span == nil || d.Span.StartByte > len(test.source) || d.Span.EndByte > len(test.source) {
				t.Errorf("invalid error span: %+v", d)
			}
		}
	}
}

func TestMapOperationsBuiltinShadowingAndBranches(t *testing.T) {
	source := `package main
func main() {
 close := func(value int) {}
 make := func(value int) int { return value }
 close(make(1))
	data := map[string]int{"a": 1}
	for i := 0; i < 3; i++ { if i == 2 { break }; data["a"]++; data["a"] += i }
	_ = data["a"]
	box := struct { data map[string]int }{data}
	box.data["a"]++
}`
	model := mustAnalyze(t, source)
	kinds := map[string]int{}
	for _, node := range model.Nodes {
		kinds[node.Kind]++
	}
	if kinds["close"] != 0 || kinds["make_channel"] != 0 {
		t.Fatal("shadowed builtin was mistaken for a channel operation")
	}
	if kinds["map_write"] != 3 || kinds["break"] != 1 || kinds["map_read"] != 4 {
		t.Fatalf("lost state/control operations: %+v", kinds)
	}
}

func TestRangeOverNamedChannelType(t *testing.T) {
	source := `package main
type Stream chan int
func consume(stream Stream) { for value := range stream { _ = value } }
func main() { stream := make(Stream, 1); stream <- 42; close(stream); consume(stream) }`
	model := mustAnalyze(t, source)
	for _, node := range model.Nodes {
		if node.Kind == "receive" && node.Label == "range stream" && node.ChannelID != "" {
			return
		}
	}
	t.Fatal("channel range must expose its implicit receive")
}

func TestSourceHashAndByteRanges(t *testing.T) {
	a := Analyze("package main\nfunc main() { ch := make(chan int); close(ch) }")
	b := Analyze("package main\n// 中文\nfunc main() { ch := make(chan int); close(ch) }")
	if a.SourceHash == b.SourceHash {
		t.Fatal("source hash must invalidate positions after editing")
	}
	source := "package main\nfunc main() { ch := make(chan int); ch <- \"你好\" }"
	r := Analyze(source)
	if r.Status != "type_error" || len(r.Diagnostics) == 0 {
		t.Fatal("expected channel send type error")
	}
	if r.Diagnostics[0].Span.StartByte != strings.Index(source, "\"你好\"") {
		t.Errorf("diagnostic does not identify the invalid value: %+v", r.Diagnostics[0])
	}
}

func TestAliasesRemainDeclarationsNotRuntimeChannelIdentities(t *testing.T) {
	source := `package main
type Inbox chan int
type Box struct { input Inbox; output chan int }
func relay(ch chan int) { ch <- 1 }
func choose(ch chan int) chan int { return ch }
func main() {
 original := make(chan int, 4)
 alias := original
 one := Box{input: Inbox(original), output: alias}
 two := Box{input: Inbox(alias), output: original}
 original <- 1
 alias <- 2
 one.input <- 3
 two.input <- 4
 one.output <- 5
 two.output <- 6
 streams := []chan int{original, alias}
 streams[0] <- 7
 choose(original) <- 8
 indirect := relay
 go indirect(alias)
 go relay(original)
}`
	model := mustAnalyze(t, source)
	channels := map[string]string{}
	for _, channel := range model.Channels {
		if channel.Name != "ch" {
			channels[channel.Name] = channel.ID
		}
	}
	if channels["original"] == "" || channels["alias"] == "" || channels["original"] == channels["alias"] {
		t.Fatal("distinct alias declarations must not be conflated with runtime channel identity")
	}
	if channels["input"] == "" || channels["output"] == "" || channels["input"] == channels["output"] {
		t.Fatal("struct fields must preserve their own declaration identities")
	}
	expected := map[string]string{
		"original <- 1":         channels["original"],
		"alias <- 2":            channels["alias"],
		"one.input <- 3":        channels["input"],
		"two.input <- 4":        channels["input"],
		"one.output <- 5":       channels["output"],
		"two.output <- 6":       channels["output"],
		"streams[0] <- 7":       "",
		"choose(original) <- 8": "",
	}
	seen := map[string]bool{}
	for _, node := range model.Nodes {
		text := source[node.Span.StartByte:node.Span.EndByte]
		if want, ok := expected[text]; ok && node.Kind == "send" {
			seen[text] = true
			if node.ChannelID != want {
				t.Errorf("%s: wanted declaration %q, got %q", text, want, node.ChannelID)
			}
		}
		if text == "go indirect(alias)" && node.TargetFunctionID != "" {
			t.Errorf("function variable was incorrectly resolved as static dispatch: %+v", node)
		}
		if text == "go relay(original)" && node.TargetFunctionID == "" {
			t.Errorf("direct function declaration should be resolved: %+v", node)
		}
	}
	if len(seen) != len(expected) {
		t.Fatalf("missing send operations: saw %v", seen)
	}
}

func TestShadowedPackageBuiltinIsAnOrdinaryCall(t *testing.T) {
	source := `package main
func close(ch chan int) {}
func main() {
 ch := make(chan int)
 close(ch)
 callback := close
 callback(ch)
}`
	model := mustAnalyze(t, source)
	foundDirect, foundIndirect := false, false
	for _, node := range model.Nodes {
		if node.Kind == "close" {
			t.Fatal("package function named close must not be interpreted as the builtin")
		}
		text := source[node.Span.StartByte:node.Span.EndByte]
		if node.Kind == "call" && text == "close(ch)" {
			foundDirect = true
			if node.TargetFunctionID == "" || node.Opaque {
				t.Errorf("direct source function lost its target: %+v", node)
			}
		}
		if node.Kind == "call" && text == "callback(ch)" {
			foundIndirect = true
			if node.TargetFunctionID != "" || !node.Opaque {
				t.Errorf("dynamic call must stay opaque without dataflow analysis: %+v", node)
			}
		}
	}
	if !foundDirect || !foundIndirect {
		t.Fatal("missing ordinary call nodes")
	}
}
