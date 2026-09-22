package engine

import (
	"fmt"
	"go/token"
	"math/big"
	"strconv"
	"strings"
)

// Validate checks the closed v0 language, symbol identities and lexical scope.
// Go's remaining language rules (including unused variables) run in Generate.
func Validate(p *Program) []Diagnostic {
	v := &validator{ids: map[string]bool{}, functions: map[string]*Function{}, symbols: map[string]*Symbol{}, declared: map[string]bool{}, blocks: map[string]bool{}}
	if p == nil {
		v.fail("IR_REQUIRED", "", "A program is required.")
		return v.diagnostics
	}
	if p.SchemaVersion != "1.0" {
		v.fail("IR_VERSION", p.ID, "Unsupported schema version.")
	}
	if p.PackageName != "main" {
		v.fail("IR_PACKAGE", p.ID, "The package must be main.")
	}
	v.addID(p.ID)
	for i := range p.Functions {
		f := &p.Functions[i]
		v.addID(f.ID)
		v.addID(f.Body.ID)
		v.blocks[f.Body.ID] = true
		v.functions[f.ID] = f
		for j := range f.Body.Statements {
			s := &f.Body.Statements[j]
			v.addID(s.ID)
			if s.Value != nil {
				v.addID(s.Value.ID)
			}
		}
	}
	for i := range p.Symbols {
		s := &p.Symbols[i]
		v.addID(s.ID)
		v.symbols[s.ID] = s
		if !token.IsIdentifier(s.Name) || s.Name == "_" {
			v.fail("IR_IDENTIFIER", s.ID, "A symbol needs a valid, non-blank Go identifier.")
		}
		if s.Type != "int" && s.Type != "bool" && s.Type != "chan int" {
			v.fail("IR_TYPE", s.ID, "Unsupported symbol type.")
		}
		if !v.blocks[s.ScopeID] {
			v.fail("IR_SCOPE", s.ID, "The symbol's declaration block does not exist.")
		}
	}
	if len(v.ids) > MaxNodes {
		v.fail("IR_NODE_LIMIT", p.ID, fmt.Sprintf("The program exceeds the configured limit of %d nodes.", MaxNodes))
	}
	// Structural failures must not reach recursive traversal.
	if len(v.diagnostics) > 0 {
		return v.diagnostics
	}
	entry := v.functions[p.EntryFunctionID]
	if entry == nil || entry.Kind != "main" {
		v.fail("IR_ENTRY", p.ID, "The entry function must reference main.")
	}
	mains := 0
	incoming := map[string]int{}
	for _, f := range v.functions {
		if f.Kind == "main" {
			mains++
			if f.ParentFunctionID != nil {
				v.fail("IR_PARENT", f.ID, "main must have no parent function.")
			}
			if f.ID != p.EntryFunctionID {
				v.fail("IR_ENTRY", f.ID, "main must be the entry function.")
			}
		} else if f.Kind == "goroutine" {
			if f.ParentFunctionID == nil || v.functions[valueOf(f.ParentFunctionID)] == nil {
				v.fail("IR_PARENT", f.ID, "A goroutine needs an existing lexical parent.")
			}
		} else {
			v.fail("IR_KIND", f.ID, "Unknown function kind.")
		}
		for _, s := range f.Body.Statements {
			if s.Kind == "spawn" {
				target := v.functions[s.FunctionID]
				if target == nil {
					v.fail("IR_FUNCTION_REFERENCE", s.ID, "The spawned function does not exist.")
					continue
				}
				incoming[target.ID]++
				if target.Kind != "goroutine" || target.ParentFunctionID == nil || *target.ParentFunctionID != f.ID {
					v.fail("IR_PARENT", s.ID, "A spawn must reference a goroutine with this lexical parent.")
				}
			}
		}
	}
	if mains != 1 {
		v.fail("IR_ENTRY", p.ID, "The program must have exactly one main function.")
	}
	for _, f := range v.functions {
		if f.Kind == "goroutine" && incoming[f.ID] != 1 {
			v.fail("IR_SPAWN", f.ID, "Each goroutine must be referenced by exactly one spawn.")
		}
		seen := map[string]bool{}
		current := f
		for current != nil {
			if seen[current.ID] {
				v.fail("IR_FUNCTION_CYCLE", f.ID, "Function parent relationships contain a cycle.")
				break
			}
			seen[current.ID] = true
			if current.ParentFunctionID == nil {
				break
			}
			current = v.functions[*current.ParentFunctionID]
		}
	}
	if len(v.diagnostics) > 0 {
		return v.diagnostics
	}
	v.function(entry, map[string]bool{}, map[string]string{})
	for _, s := range p.Symbols {
		if !v.declared[s.ID] {
			v.fail("IR_UNDECLARED_SYMBOL", s.ID, "The symbol has no declaration in its block.")
		}
	}
	return v.diagnostics
}

type validator struct {
	diagnostics []Diagnostic
	ids         map[string]bool
	blocks      map[string]bool
	functions   map[string]*Function
	symbols     map[string]*Symbol
	declared    map[string]bool
}

func (v *validator) fail(code, id, message string) {
	v.diagnostics = append(v.diagnostics, Diagnostic{Code: code, Severity: "error", Phase: "ir", Message: message, NodeID: id})
}
func (v *validator) addID(id string) {
	if strings.TrimSpace(id) == "" {
		v.fail("IR_ID", "", "IDs must be nonempty.")
	}
	if v.ids[id] {
		v.fail("IR_DUPLICATE_ID", id, "All IDs must be unique.")
	}
	v.ids[id] = true
}
func valueOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func (v *validator) function(f *Function, inherited map[string]bool, inheritedNames map[string]string) {
	available := map[string]bool{}
	names := map[string]string{}
	localNames := map[string]bool{}
	for id, b := range inherited {
		available[id] = b
	}
	for name, id := range inheritedNames {
		names[name] = id
	}
	reference := func(id, nodeID string) string {
		sym := v.symbols[id]
		if sym == nil {
			v.fail("IR_SYMBOL_REFERENCE", nodeID, "The referenced symbol does not exist.")
			return ""
		}
		if !available[id] {
			v.fail("IR_NOT_VISIBLE", nodeID, "The symbol must be declared before this use in a visible lexical scope.")
			return ""
		}
		if names[sym.Name] != id {
			v.fail("IR_SHADOWED_REFERENCE", nodeID, fmt.Sprintf("%s is shadowed here; this name would refer to a different symbol.", sym.Name))
			return ""
		}
		if sym.ScopeID != f.Body.ID && sym.Type != "chan int" {
			v.fail("IR_CAPTURE", nodeID, "Only channels may be captured from an outer goroutine.")
			return ""
		}
		return sym.Type
	}
	declare := func(id, nodeID, want string) {
		sym := v.symbols[id]
		if sym == nil {
			v.fail("IR_SYMBOL_REFERENCE", nodeID, "The declared symbol does not exist.")
			return
		}
		if sym.ScopeID != f.Body.ID {
			v.fail("IR_SCOPE", nodeID, "The symbol's scope must be this declaration block.")
		}
		if v.declared[id] || localNames[sym.Name] {
			v.fail("IR_REDECLARATION", nodeID, "Every binding must introduce a new symbol and name in this block.")
		}
		if want != "" && sym.Type != want {
			v.fail("IR_BINDING_TYPE", nodeID, fmt.Sprintf("This binding must have type %s.", want))
		}
		v.declared[id] = true
		available[id] = true
		names[sym.Name] = id
		localNames[sym.Name] = true
	}
	expression := func(e *Expr, nodeID string) string {
		if e == nil {
			v.fail("IR_EXPRESSION", nodeID, "An expression is required.")
			return ""
		}
		switch e.Kind {
		case "int_literal":
			value, ok := e.Value.(string)
			if !ok || !decimal(value) {
				v.fail("IR_LITERAL", e.ID, "Integer literals must be decimal strings.")
				return ""
			}
			n, _ := new(big.Int).SetString(value, 10)
			max := new(big.Int).Lsh(big.NewInt(1), uint(strconv.IntSize-1))
			min := new(big.Int).Neg(new(big.Int).Set(max))
			max.Sub(max, big.NewInt(1))
			if n.Cmp(min) < 0 || n.Cmp(max) > 0 {
				v.fail("IR_INT_RANGE", e.ID, fmt.Sprintf("Integer literal is outside the target %d-bit int range.", strconv.IntSize))
			}
			return "int"
		case "bool_literal":
			if _, ok := e.Value.(bool); !ok {
				v.fail("IR_LITERAL", e.ID, "Boolean literals must be JSON booleans.")
			}
			return "bool"
		case "variable":
			t := reference(e.SymbolID, e.ID)
			if t == "chan int" {
				v.fail("IR_EXPRESSION_TYPE", e.ID, "Only int and bool variables are value expressions.")
				return ""
			}
			return t
		default:
			v.fail("IR_KIND", e.ID, "Unknown expression kind.")
			return ""
		}
	}
	channel := func(s Statement) {
		if t := reference(s.ChannelSymbolID, s.ID); t != "" && t != "chan int" {
			v.fail("IR_CHANNEL_TYPE", s.ID, "Channel operations require chan int.")
		}
	}
	for _, s := range f.Body.Statements {
		switch s.Kind {
		case "make_channel":
			if s.Capacity < 0 || s.Capacity > MaxCapacity {
				v.fail("IR_CAPACITY", s.ID, fmt.Sprintf("Channel capacity must be between 0 and %d.", MaxCapacity))
			}
			declare(s.SymbolID, s.ID, "chan int")
		case "let":
			t := expression(s.Value, s.ID)
			declare(s.SymbolID, s.ID, t)
		case "spawn":
			v.function(v.functions[s.FunctionID], available, names)
		case "send":
			channel(s)
			if t := expression(s.Value, s.ID); t != "" && t != "int" {
				v.fail("IR_SEND_TYPE", s.ID, "A chan int can only send int expressions.")
			}
		case "receive":
			channel(s)
			if len(s.Bindings) > 2 {
				v.fail("IR_RECEIVE", s.ID, "Receive bindings must have length 0, 1 or 2.")
				continue
			}
			count := 0
			for i, id := range s.Bindings {
				if id == nil {
					continue
				}
				count++
				typ := "int"
				if i == 1 {
					typ = "bool"
				}
				declare(*id, s.ID, typ)
			}
			if len(s.Bindings) > 0 && count == 0 {
				v.fail("IR_RECEIVE", s.ID, "A receiving short declaration needs at least one non-blank binding.")
			}
		case "close":
			channel(s)
		case "print":
			expression(s.Value, s.ID)
		case "return":
		default:
			v.fail("IR_KIND", s.ID, "Unknown statement kind.")
		}
	}
}
func decimal(s string) bool {
	if strings.HasPrefix(s, "-") {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func canonicalInteger(s string) string {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return s
	}
	return n.String()
}
