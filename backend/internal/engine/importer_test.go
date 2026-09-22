package engine

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func example(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../../../examples/" + name + ".go")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestImportExamplesAndBindings(t *testing.T) {
	for _, name := range []string{"demo", "buffered", "shadowing", "nested"} {
		t.Run(name, func(t *testing.T) {
			source := example(t, name)
			result := Import(source)
			if result.Status != "editable" {
				t.Fatalf("status %s: %+v", result.Status, result.Diagnostics)
			}
			if result.Source != source || result.SourceHash != sourceDigest(source) {
				t.Fatal("source or source hash changed")
			}
			if diagnostics := Validate(result.IR); len(diagnostics) != 0 {
				t.Fatalf("invalid lowered IR: %+v", diagnostics)
			}
			for id, span := range result.SourceMap {
				if span.File != "main.go" || span.StartByte < 0 || span.EndByte > len(source) || span.StartByte >= span.EndByte || span.StartLine < 1 || span.StartColumn < 1 {
					t.Fatalf("bad span for %s: %+v", id, span)
				}
			}
			for _, fn := range result.IR.Functions {
				if _, ok := result.SourceMap[fn.ID]; !ok {
					t.Errorf("function mapping missing: %s", fn.ID)
				}
				if _, ok := result.SourceMap[fn.Body.ID]; !ok {
					t.Errorf("block mapping missing: %s", fn.Body.ID)
				}
			}
			for _, symbol := range result.IR.Symbols {
				span, ok := result.SourceMap[symbol.ID]
				if !ok || source[span.StartByte:span.EndByte] != symbol.Name {
					t.Errorf("symbol mapping incorrect: %+v", symbol)
				}
			}
		})
	}
	demo := Import(example(t, "demo")).IR
	main, child := demo.Functions[0], demo.Functions[1]
	channel := main.Body.Statements[0].SymbolID
	if main.Body.Statements[2].ChannelSymbolID != channel || child.Body.Statements[0].ChannelSymbolID != channel {
		t.Fatal("channel capture lost its object binding")
	}
	if child.ParentFunctionID == nil || *child.ParentFunctionID != main.ID {
		t.Fatal("child has wrong lexical parent")
	}
	if child.Body.Statements[0].Value.Value != "42" {
		t.Fatal("integer not represented as decimal string")
	}
	shadow := Import(example(t, "shadowing")).IR
	outer, inner := shadow.Functions[0].Body.Statements, shadow.Functions[1].Body.Statements
	if outer[0].SymbolID == inner[0].SymbolID || inner[1].ChannelSymbolID != inner[0].SymbolID || outer[3].ChannelSymbolID != outer[0].SymbolID {
		t.Fatal("shadowed channel references were merged")
	}
}

func TestImportStatusAndDiagnosticPositions(t *testing.T) {
	tests := []struct{ name, source, status string }{
		{"syntax", "package main; func main( {", "invalid"},
		{"type", example(t, "invalid-type"), "invalid"},
		{"scope", "package main; func main() { close(ch) }", "invalid"},
		{"unused", "package main; func main() { x := 1 }", "invalid"},
		{"missing-main", "package main", "invalid"},
		{"bad-main-signature", "package main; func main(x int) {}", "invalid"},
		{"select", example(t, "unsupported-select"), "unsupported"},
		{"nil-channel", "package main; func main() { var ch chan int; close(ch) }", "unsupported"},
		{"named-function", "package main; func worker() {}; func main() { go worker() }", "unsupported"},
		{"capture-int", "package main; import \"fmt\"; func main() { x := 1; go func(){ fmt.Println(x) }() }", "unsupported"},
		{"capture-bool", "package main; import \"fmt\"; func main() { x := true; go func(){ fmt.Println(x) }() }", "unsupported"},
		{"reassignment", "package main; func main() { ch := make(chan int); ch = make(chan int); close(ch) }", "unsupported"},
		{"receive-existing", "package main; import \"fmt\"; func main() { ch := make(chan int); v := 1; v, ok := <-ch; fmt.Println(v); fmt.Println(ok) }", "unsupported"},
		{"channel-alias", "package main; func main() { ch := make(chan int); copy := ch; close(copy) }", "unsupported"},
		{"arithmetic", "package main; import \"fmt\"; func main() { fmt.Println(1+2) }", "unsupported"},
		{"hexadecimal", "package main; import \"fmt\"; func main() { fmt.Println(0x2A) }", "unsupported"},
		{"octal", "package main; import \"fmt\"; func main() { fmt.Println(052) }", "unsupported"},
		{"receive-argument", "package main; import \"fmt\"; func main() { ch := make(chan int); fmt.Println(<-ch) }", "unsupported"},
		{"capacity-expression", "package main; func main() { ch := make(chan int, 1+1); close(ch) }", "unsupported"},
		{"capacity-negative", "package main; func main() { ch := make(chan int, -1); close(ch) }", "invalid"},
		{"capacity-limit", "package main; func main() { ch := make(chan int, 1025); close(ch) }", "unsupported"},
		{"send-only", "package main; func main() { ch := make(chan<- int); close(ch) }", "unsupported"},
		{"channel-bool", "package main; func main() { ch := make(chan bool); close(ch) }", "unsupported"},
		{"external-dependency", "package main; import \"example.invalid/dependency\"; func main() {}", "unsupported"},
		{"alias-import", "package main; import f \"fmt\"; func main() { f.Println(1) }", "unsupported"},
		{"dot-import", "package main; import . \"fmt\"; func main() { Println(1) }", "unsupported"},
		{"package", "package other; func main() {}", "unsupported"},
		{"go-directive", "package main\n//go:noinline\nfunc main() {}", "unsupported"},
		{"line-directive", "package main\n//line elsewhere.go:100\nfunc main() {}", "unsupported"},
		{"block-line-directive", "package main\n/*line elsewhere.go:100*/\nfunc main() {}", "unsupported"},
		{"build-tag", "//go:build linux\n\npackage main\nfunc main() {}", "unsupported"},
		{"old-build-tag", "// +build linux\n\npackage main\nfunc main() {}", "unsupported"},
		{"invalid-utf8", "package main\n//\xff\nfunc main(){}", "invalid"},
		{"oversized", strings.Repeat(" ", MaxSourceBytes+1), "unsupported"},
		{"int-overflow", "package main; import \"fmt\"; func main() { fmt.Println(9223372036854775808) }", "invalid"},
		{"fake-close", "package main; func main() { close := func(x int){}; close(1) }", "unsupported"},
		{"fake-make", "package main; func main() { make := func() int{return 1}; x := make(); _ = x }", "unsupported"},
		{"fake-fmt", "package main; func main() { fmt := struct{Println func(int)}{func(int){}}; fmt.Println(1) }", "unsupported"},
		{"invalid-before-subset", "package main; func main() { if true { missing() } }", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Import(tt.source)
			if result.Status != tt.status {
				t.Fatalf("expected %s, got %s: %+v", tt.status, result.Status, result.Diagnostics)
			}
			if result.IR != nil || result.Source != tt.source || len(result.Diagnostics) == 0 {
				t.Fatal("unsuccessful import must preserve source and diagnostics, without partial IR")
			}
			for _, d := range result.Diagnostics {
				if d.Span == nil || d.Span.StartByte < 0 || d.Span.EndByte > len(tt.source) || d.Span.StartLine < 1 || d.Span.StartColumn < 1 {
					t.Fatalf("missing or invalid source position: %+v", d)
				}
			}
		})
	}
}

func TestImportSupportedLiteralsAndReceiveSlots(t *testing.T) {
	source := `package main
import "fmt"
func main() {
 ch := make(chan int, (0))
 go func() { ch <- (1_000); ch <- -(42); close(ch) }()
 _, ok := (<-ch)
 value, _ := <-ch
 <-ch
 fmt.Println(ok)
 fmt.Println(value)
 minimum := -9223372036854775808
 fmt.Println(minimum)
 true := false
 fmt.Println(true)
 return
}`
	r := Import(source)
	if r.Status != "editable" {
		t.Fatalf("%s: %+v", r.Status, r.Diagnostics)
	}
	main := r.IR.Functions[0].Body.Statements
	if main[0].Capacity != 0 {
		t.Fatal("explicit zero was not normalized")
	}
	if len(main[2].Bindings) != 2 || main[2].Bindings[0] != nil || main[2].Bindings[1] == nil {
		t.Fatal("blank first receive binding lost")
	}
	if len(main[3].Bindings) != 2 || main[3].Bindings[0] == nil || main[3].Bindings[1] != nil {
		t.Fatal("blank second receive binding lost")
	}
	if len(main[4].Bindings) != 0 {
		t.Fatal("discard receive should have zero bindings")
	}
	child := r.IR.Functions[1].Body.Statements
	if child[0].Value.Value != "1000" || child[1].Value.Value != "-42" {
		t.Fatal("decimal literals not normalized")
	}
	if main[9].Kind != "let" || main[9].Value.Value != false || main[10].Value.Kind != "variable" {
		t.Fatal("shadowed true resolved as a literal")
	}
}

func TestImportUTF8PhysicalSourceMap(t *testing.T) {
	source := "package main\n// 中文注释\nfunc main() {\n ch := make(chan int)\n go func() { ch <- 42 }()\n <-ch\n}\n"
	r := Import(source)
	if r.Status != "editable" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	send := r.IR.Functions[1].Body.Statements[0]
	span := r.SourceMap[send.ID]
	if source[span.StartByte:span.EndByte] != "ch <- 42" || span.StartLine != 5 || span.StartColumn != 14 {
		t.Fatalf("incorrect UTF-8 source mapping: %+v", span)
	}
	expr := r.SourceMap[send.Value.ID]
	if source[expr.StartByte:expr.EndByte] != "42" {
		t.Fatalf("incorrect expression span: %+v", expr)
	}
}

func TestImportNodeLimit(t *testing.T) {
	source := "package main; func main() {" + strings.Repeat("return;", MaxNodes+1) + "}"
	r := Import(source)
	if r.Status != "unsupported" || r.IR != nil || len(r.Diagnostics) == 0 {
		t.Fatalf("node limit not enforced: %+v", r)
	}
}

func TestImportReferenceFixture(t *testing.T) {
	data, err := os.ReadFile("../../../examples/demo.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference Program
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	imported := Import(example(t, "demo"))
	if imported.Status != "editable" {
		t.Fatalf("%+v", imported.Diagnostics)
	}
	if Normalize(imported.IR) != Normalize(&reference) {
		t.Fatal("the imported example disagrees with the design's normative fixture")
	}
}

func TestImportRoundTripBoundaryCases(t *testing.T) {
	sources := []string{
		`package main; func main(){}`,
		`package main; func main(){ return; return }`,
		`package main; import "fmt"; func main(){ fmt.Println(-0) }`,
		`package main; import "fmt"; func main(){ true := true; fmt.Println(true) }`,
		`package main; import "fmt"; func main(){ false := false; fmt.Println(false) }`,
		`package main; import "fmt"; func main(){ make := 1; fmt.Println(make) }`,
		`package main; import "fmt"; func main(){ close := 1; fmt.Println(close) }`,
		`package main; import "fmt"; func main(){ int := 1; fmt.Println(int) }`,
		`package main; func main(){ fmt := make(chan int); close(fmt) }`,
		`package main; func main(){ ch := make((chan int), (1)); (ch) <- (42); (<-(ch)); (close)((ch)) }`,
		`package main; import "fmt"; func main(){ ch := make(chan int); go (func(){ ch := <-ch; fmt.Println(ch) })(); ch <- 42 }`,
		`package main; import "fmt"; func main(){ x := 1; go func(){ x := true; fmt.Println(x) }(); fmt.Println(x) }`,
		`package main; import "fmt"; func main(){ ch := make(chan int); go func(){ go func(){ ch := make(chan int, 1); ch <- 1; v := <-ch; fmt.Println(v) }(); ch <- 42 }(); <-ch }`,
		`package main; import "fmt"; func main(){ 最小值 := -9223372036854775808; fmt.Println(最小值) }`,
	}
	for _, source := range sources {
		t.Run(source, func(t *testing.T) { assertImportRoundTrip(t, source, true) })
	}
}

func assertImportRoundTrip(t *testing.T, source string, requireEditable bool) {
	t.Helper()
	imported := Import(source)
	if imported.Status != "editable" {
		if requireEditable {
			t.Fatalf("expected editable: %s: %+v", imported.Status, imported.Diagnostics)
		}
		if imported.IR != nil || len(imported.Diagnostics) == 0 {
			t.Fatal("rejected source returned a partial model or no diagnosis")
		}
		return
	}
	generated := Generate(imported.IR)
	if generated.Status != "ok" {
		t.Fatalf("editable source cannot regenerate: %+v\n%s", generated.Diagnostics, source)
	}
	reimported := Import(generated.Source)
	if reimported.Status != "editable" {
		t.Fatalf("generated source cannot reimport: %+v\n%s", reimported.Diagnostics, generated.Source)
	}
	if Normalize(imported.IR) != Normalize(reimported.IR) {
		t.Fatalf("round trip changed semantics\noriginal: %s\ngenerated: %s", source, generated.Source)
	}
}

func FuzzImportRoundTrip(f *testing.F) {
	for _, name := range []string{"demo", "buffered", "shadowing", "nested", "unsupported-select", "invalid-type"} {
		data, err := os.ReadFile("../../../examples/" + name + ".go")
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(data))
	}
	f.Add("package main; func main(){}")
	f.Add("package main; import \"fmt\"; func main(){ true := false; fmt.Println(true) }")
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 16*1024 {
			t.Skip()
		}
		assertImportRoundTrip(t, source, false)
	})
}
