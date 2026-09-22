package engine

import (
	"encoding/json"
	"strconv"
)

// Normalize represents semantics independently of original IDs, table ordering,
// source formatting and integer spelling. Names remain part of the program.
func Normalize(p *Program) string {
	if p == nil {
		return "null"
	}
	functions := map[string]Function{}
	symbols := map[string]Symbol{}
	for _, f := range p.Functions {
		functions[f.ID] = f
	}
	for _, s := range p.Symbols {
		symbols[s.ID] = s
	}
	result := Program{SchemaVersion: p.SchemaVersion, ID: "program", PackageName: p.PackageName, Functions: []Function{}, Symbols: []Symbol{}}
	ids := map[string]string{p.ID: "program"}
	next := 0
	rename := func(old string) string {
		if value, ok := ids[old]; ok {
			return value
		}
		next++
		value := "id_" + strconv.Itoa(next)
		ids[old] = value
		return value
	}
	visited := map[string]bool{}
	var walk func(string, *string)
	walk = func(id string, parent *string) {
		if visited[id] {
			return
		}
		visited[id] = true
		original, ok := functions[id]
		if !ok {
			return
		}
		f := Function{ID: rename(id), Kind: original.Kind, ParentFunctionID: parent, Body: Block{ID: rename(original.Body.ID), Statements: []Statement{}}}
		index := len(result.Functions)
		result.Functions = append(result.Functions, f)
		declaration := func(id string) {
			s, ok := symbols[id]
			if !ok {
				return
			}
			s.ID = rename(id)
			s.ScopeID = f.Body.ID
			result.Symbols = append(result.Symbols, s)
		}
		for _, s := range original.Body.Statements {
			s.ID = rename(s.ID)
			if s.Value != nil {
				e := *s.Value
				e.ID = rename(e.ID)
				if e.Kind == "int_literal" {
					if value, ok := e.Value.(string); ok {
						e.Value = canonicalInteger(value)
					}
				}
				if e.SymbolID != "" {
					e.SymbolID = rename(e.SymbolID)
				}
				s.Value = &e
			}
			if s.ChannelSymbolID != "" {
				s.ChannelSymbolID = rename(s.ChannelSymbolID)
			}
			if s.SymbolID != "" {
				declaration(s.SymbolID)
				s.SymbolID = rename(s.SymbolID)
			}
			if s.Kind == "receive" {
				bindings := make([]*string, len(s.Bindings))
				for i, b := range s.Bindings {
					if b != nil {
						declaration(*b)
						name := rename(*b)
						bindings[i] = &name
					}
				}
				s.Bindings = bindings
			}
			if s.Kind == "spawn" {
				old := s.FunctionID
				s.FunctionID = rename(old)
				parentID := f.ID
				walk(old, &parentID)
			}
			f.Body.Statements = append(f.Body.Statements, s)
		}
		result.Functions[index] = f
	}
	result.EntryFunctionID = rename(p.EntryFunctionID)
	walk(p.EntryFunctionID, nil)
	bytes, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	return string(bytes)
}
