package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func generatorFixture() *Program {
	return &Program{SchemaVersion: "1.0", ID: "program_demo", PackageName: "main", EntryFunctionID: "fn_main", Functions: []Function{
		{ID: "fn_main", Kind: "main", Body: Block{ID: "block_main", Statements: []Statement{
			{ID: "stmt_make", Kind: "make_channel", SymbolID: "sym_ch"},
			{ID: "stmt_spawn", Kind: "spawn", FunctionID: "fn_sender"},
			{ID: "stmt_receive", Kind: "receive", ChannelSymbolID: "sym_ch", Bindings: []*string{StringPtr("sym_value")}},
			{ID: "stmt_print", Kind: "print", Value: &Expr{ID: "expr_print_value", Kind: "variable", SymbolID: "sym_value"}},
		}}},
		{ID: "fn_sender", Kind: "goroutine", ParentFunctionID: StringPtr("fn_main"), Body: Block{ID: "block_sender", Statements: []Statement{
			{ID: "stmt_send", Kind: "send", ChannelSymbolID: "sym_ch", Value: &Expr{ID: "expr_42", Kind: "int_literal", Value: "42"}},
		}}},
	}, Symbols: []Symbol{{ID: "sym_ch", Name: "ch", Type: "chan int", ScopeID: "block_main"}, {ID: "sym_value", Name: "value", Type: "int", ScopeID: "block_main"}}}
}
func TestGenerateASTAndFormattedMapping(t *testing.T) {
	p := generatorFixture()
	result := Generate(p)
	if result.Status != "ok" {
		t.Fatalf("generate failed: %+v", result.Diagnostics)
	}
	for id, want := range map[string]string{"stmt_make": "ch := make(chan int)", "stmt_send": "ch <- 42", "expr_42": "42", "sym_value": "value", "stmt_receive": "value := <-ch"} {
		span, ok := result.SourceMap[id]
		if !ok {
			t.Fatalf("missing map %s", id)
		}
		if got := result.Source[span.StartByte:span.EndByte]; got != want {
			t.Errorf("%s maps to %q, want %q", id, got, want)
		}
		if span.StartLine < 1 || span.StartColumn < 1 {
			t.Errorf("invalid line and column: %+v", span)
		}
	}
	p.Functions[0].Body.Statements[0].Capacity = 1
	if generated := Generate(p); generated.Status != "ok" || !strings.Contains(generated.Source, "make(chan int, 1)") {
		t.Fatalf("buffered generation: %+v", generated)
	}
	p.Functions[1].Body.Statements[0].Value.Value = "-00042"
	if generated := Generate(p); generated.Status != "ok" || !strings.Contains(generated.Source, "ch <- -42") {
		t.Fatalf("negative generation: %+v", generated)
	}
}
func TestNormalizeIgnoresIDsAndTableOrder(t *testing.T) {
	p := generatorFixture()
	base := Normalize(p)
	data, _ := json.Marshal(p)
	changed := strings.ReplaceAll(string(data), "fn_", "new_fn_")
	changed = strings.ReplaceAll(changed, "sym_", "new_sym_")
	changed = strings.ReplaceAll(changed, "stmt_", "new_stmt_")
	var other Program
	if err := json.Unmarshal([]byte(changed), &other); err != nil {
		t.Fatal(err)
	}
	other.Functions[0], other.Functions[1] = other.Functions[1], other.Functions[0]
	other.Symbols[0], other.Symbols[1] = other.Symbols[1], other.Symbols[0]
	other.Functions[0].Body.Statements[0].Value.Value = "00042"
	if got := Normalize(&other); got != base {
		t.Fatalf("normalization differs:\n%s\n%s", base, got)
	}
	other.Functions[0].Body.Statements[0].Value.Value = "43"
	if Normalize(&other) == base {
		t.Fatal("normalization lost the literal value")
	}
}
func TestRejectInvalidIR(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Program)
		code string
	}{
		{"dangling", func(p *Program) { p.Functions[1].Body.Statements[0].ChannelSymbolID = "gone" }, "IR_SYMBOL_REFERENCE"},
		{"capacity", func(p *Program) { p.Functions[0].Body.Statements[0].Capacity = MaxCapacity + 1 }, "IR_CAPACITY"},
		{"before declaration", func(p *Program) {
			b := &p.Functions[0].Body
			b.Statements[0], b.Statements[1] = b.Statements[1], b.Statements[0]
		}, "IR_NOT_VISIBLE"},
		{"send bool", func(p *Program) {
			p.Functions[1].Body.Statements[0].Value.Kind = "bool_literal"
			p.Functions[1].Body.Statements[0].Value.Value = true
		}, "IR_SEND_TYPE"},
		{"int overflow", func(p *Program) { p.Functions[1].Body.Statements[0].Value.Value = "9223372036854775808" }, "IR_INT_RANGE"},
		{"blank receive", func(p *Program) { p.Functions[0].Body.Statements[2].Bindings = []*string{nil, nil} }, "IR_RECEIVE"},
		{"bad name", func(p *Program) { p.Symbols[0].Name = "for" }, "IR_IDENTIFIER"},
		{"duplicate ID", func(p *Program) { p.Symbols[0].ID = p.ID }, "IR_DUPLICATE_ID"},
		{"parent cycle", func(p *Program) { p.Functions[1].ParentFunctionID = StringPtr("fn_sender") }, "IR_FUNCTION_CYCLE"},
		{"captured value", func(p *Program) {
			p.Functions[0].Body.Statements = append([]Statement{{ID: "value_let", Kind: "let", SymbolID: "number", Value: &Expr{ID: "number_expr", Kind: "int_literal", Value: "42"}}}, p.Functions[0].Body.Statements...)
			p.Symbols = append(p.Symbols, Symbol{ID: "number", Name: "n", Type: "int", ScopeID: "block_main"})
			p.Functions[1].Body.Statements[0].Value = &Expr{ID: "expr_42", Kind: "variable", SymbolID: "number"}
		}, "IR_CAPTURE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := generatorFixture()
			tc.edit(p)
			r := Generate(p)
			if r.Status != "invalid_ir" {
				t.Fatalf("accepted invalid IR: %s", r.Source)
			}
			for _, d := range r.Diagnostics {
				if d.Code == tc.code {
					return
				}
			}
			t.Fatalf("missing %s in %+v", tc.code, r.Diagnostics)
		})
	}
}
func TestGeneratedNameResolution(t *testing.T) {
	p := generatorFixture()
	// A channel called true is legal until a later bool literal needs the predeclared true.
	p.Symbols[0].Name = "true"
	if r := Generate(p); r.Status != "ok" {
		t.Fatalf("valid predeclared shadow rejected: %+v", r.Diagnostics)
	}
	p.Functions[1].Body.Statements = append(p.Functions[1].Body.Statements, Statement{ID: "literal_print", Kind: "print", Value: &Expr{ID: "true_literal", Kind: "bool_literal", Value: true}})
	if r := Generate(p); r.Status != "invalid_ir" {
		t.Fatal("literal silently rebound to channel variable")
	}
	// Binding to an outer channel is impossible after a same-named declaration.
	p = generatorFixture()
	p.Symbols = append(p.Symbols, Symbol{ID: "inner_ch", Name: "ch", Type: "chan int", ScopeID: "block_sender"})
	p.Functions[1].Body.Statements = append([]Statement{{ID: "inner_make", Kind: "make_channel", SymbolID: "inner_ch"}}, p.Functions[1].Body.Statements...)
	if r := Generate(p); r.Status != "invalid_ir" {
		t.Fatal("outer channel silently rebound to inner channel")
	}
	p.Functions[1].Body.Statements[1].ChannelSymbolID = "inner_ch"
	if r := Generate(p); r.Status != "ok" {
		t.Fatalf("valid lexical shadow rejected: %+v", r.Diagnostics)
	}
}
func TestUnusedLocalIsRejectedByGoTypes(t *testing.T) {
	p := generatorFixture()
	p.Functions[0].Body.Statements = p.Functions[0].Body.Statements[:3]
	result := Generate(p)
	if result.Status != "invalid_ir" {
		t.Fatal("unused local was accepted")
	}
	found := false
	for _, d := range result.Diagnostics {
		if d.Phase == "types" && strings.Contains(d.Message, "not used") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing real type diagnostic: %+v", result.Diagnostics)
	}
}
func TestReceiveVariants(t *testing.T) {
	for _, kind := range []string{"discard", "value", "both", "ignore_value", "ignore_ok"} {
		t.Run(kind, func(t *testing.T) {
			p := generatorFixture()
			p.Functions[1].Body.Statements = append(p.Functions[1].Body.Statements, Statement{ID: "sender_close", Kind: "close", ChannelSymbolID: "sym_ch"})
			recv := &p.Functions[0].Body.Statements[2]
			switch kind {
			case "discard":
				recv.Bindings = []*string{}
				p.Functions[0].Body.Statements = p.Functions[0].Body.Statements[:3]
				p.Symbols = p.Symbols[:1]
			case "both", "ignore_value":
				p.Symbols = append(p.Symbols, Symbol{ID: "sym_ok", Name: "ok", Type: "bool", ScopeID: "block_main"})
				recv.Bindings = append(recv.Bindings, StringPtr("sym_ok"))
				p.Functions[0].Body.Statements = append(p.Functions[0].Body.Statements, Statement{ID: "print_ok", Kind: "print", Value: &Expr{ID: "ok_expr", Kind: "variable", SymbolID: "sym_ok"}})
				if kind == "ignore_value" {
					recv.Bindings[0] = nil
					p.Symbols = append(p.Symbols[:1], p.Symbols[2:]...)
					p.Functions[0].Body.Statements = append(p.Functions[0].Body.Statements[:3], p.Functions[0].Body.Statements[4:]...)
				}
			case "ignore_ok":
				recv.Bindings = append(recv.Bindings, nil)
			}
			if result := Generate(p); result.Status != "ok" {
				t.Fatalf("receive failed: %+v", result.Diagnostics)
			}
		})
	}
}
