package engine

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// Generate constructs Go syntax, formats it, and reparses the final bytes before
// building a source map. No position from the synthetic AST is exposed.
func Generate(p *Program) Result {
	result := Result{Status: "invalid_ir", Diagnostics: []Diagnostic{}}
	if diagnostics := Validate(p); len(diagnostics) > 0 {
		result.Diagnostics = diagnostics
		return result
	}
	g := &generator{functions: map[string]*Function{}, symbols: map[string]*Symbol{}}
	for i := range p.Functions {
		f := &p.Functions[i]
		g.functions[f.ID] = f
	}
	for i := range p.Symbols {
		s := &p.Symbols[i]
		g.symbols[s.ID] = s
	}
	file := &ast.File{Name: ast.NewIdent("main")}
	main := &ast.FuncDecl{Name: ast.NewIdent("main"), Type: &ast.FuncType{Params: &ast.FieldList{}}, Body: g.block(g.functions[p.EntryFunctionID])}
	if g.usesPrint {
		file.Decls = append(file.Decls, &ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{&ast.ImportSpec{Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote("fmt")}}}})
	}
	file.Decls = append(file.Decls, main)
	var buffer bytes.Buffer
	if err := format.Node(&buffer, token.NewFileSet(), file); err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "GENERATE_FORMAT", Severity: "error", Phase: "generate", Message: err.Error()})
		return result
	}
	source := buffer.String()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "main.go", source, parser.AllErrors)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "GENERATE_PARSE", Severity: "error", Phase: "generate", Message: err.Error()})
		return result
	}
	mapping := &generatedMapping{fset: fset, g: g, spans: map[string]Span{}, definitions: map[string]*ast.Ident{}}
	mapping.add(p.ID, parsed)
	var mainDecl *ast.FuncDecl
	for _, decl := range parsed.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			mainDecl = fn
		}
	}
	if mainDecl == nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "GENERATE_STRUCTURE", Severity: "error", Phase: "generate", Message: "Generated main is missing."})
		return result
	}
	mapping.add(p.EntryFunctionID, mainDecl)
	if err = mapping.block(g.functions[p.EntryFunctionID], mainDecl.Body); err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "GENERATE_STRUCTURE", Severity: "error", Phase: "generate", Message: err.Error()})
		return result
	}
	info, diagnostics := checkGoTypes(fset, parsed)
	for i := range diagnostics {
		if diagnostics[i].NodeID == "" && diagnostics[i].Span != nil {
			diagnostics[i].NodeID = smallestNode(mapping.spans, diagnostics[i].Span.StartByte)
		}
	}
	if len(diagnostics) == 0 {
		for _, use := range mapping.uses {
			def := mapping.definitions[use.symbolID]
			if def == nil || info.Defs[def] == nil || info.Uses[use.ident] != info.Defs[def] {
				diagnostics = append(diagnostics, Diagnostic{Code: "GENERATE_BINDING", Severity: "error", Phase: "generate", Message: "The generated name would refer to a different symbol.", NodeID: use.nodeID})
			}
		}
		for _, use := range mapping.predeclared {
			if info.Uses[use.ident] != types.Universe.Lookup(use.ident.Name) {
				diagnostics = append(diagnostics, Diagnostic{Code: "GENERATE_PREDECLARED", Severity: "error", Phase: "generate", Message: fmt.Sprintf("%s is shadowed at this use.", use.ident.Name), NodeID: use.nodeID})
			}
		}
		for _, use := range mapping.packages {
			pkg, ok := info.Uses[use.ident].(*types.PkgName)
			if !ok || pkg.Imported().Path() != "fmt" {
				diagnostics = append(diagnostics, Diagnostic{Code: "GENERATE_FMT", Severity: "error", Phase: "generate", Message: "fmt.Println must refer to the standard library fmt package.", NodeID: use.nodeID})
			}
		}
	}
	result.Source = source
	result.SourceMap = mapping.spans
	result.SourceHash = sourceDigest(source)
	if len(diagnostics) > 0 {
		result.Diagnostics = diagnostics
		return result
	}
	result.Status = "ok"
	return result
}

type generator struct {
	functions map[string]*Function
	symbols   map[string]*Symbol
	usesPrint bool
}

func (g *generator) symbol(id string) *ast.Ident { return ast.NewIdent(g.symbols[id].Name) }
func (g *generator) expression(e *Expr) ast.Expr {
	switch e.Kind {
	case "variable":
		return g.symbol(e.SymbolID)
	case "bool_literal":
		return ast.NewIdent(strconv.FormatBool(e.Value.(bool)))
	default:
		value := canonicalInteger(e.Value.(string))
		if strings.HasPrefix(value, "-") {
			return &ast.UnaryExpr{Op: token.SUB, X: &ast.BasicLit{Kind: token.INT, Value: value[1:]}}
		}
		return &ast.BasicLit{Kind: token.INT, Value: value}
	}
}
func (g *generator) block(f *Function) *ast.BlockStmt {
	block := &ast.BlockStmt{List: []ast.Stmt{}}
	for _, s := range f.Body.Statements {
		var stmt ast.Stmt
		switch s.Kind {
		case "make_channel":
			args := []ast.Expr{&ast.ChanType{Dir: ast.SEND | ast.RECV, Value: ast.NewIdent("int")}}
			if s.Capacity > 0 {
				args = append(args, &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(s.Capacity)})
			}
			stmt = &ast.AssignStmt{Lhs: []ast.Expr{g.symbol(s.SymbolID)}, Tok: token.DEFINE, Rhs: []ast.Expr{&ast.CallExpr{Fun: ast.NewIdent("make"), Args: args}}}
		case "let":
			stmt = &ast.AssignStmt{Lhs: []ast.Expr{g.symbol(s.SymbolID)}, Tok: token.DEFINE, Rhs: []ast.Expr{g.expression(s.Value)}}
		case "spawn":
			stmt = &ast.GoStmt{Call: &ast.CallExpr{Fun: &ast.FuncLit{Type: &ast.FuncType{Params: &ast.FieldList{}}, Body: g.block(g.functions[s.FunctionID])}}}
		case "send":
			stmt = &ast.SendStmt{Chan: g.symbol(s.ChannelSymbolID), Value: g.expression(s.Value)}
		case "receive":
			recv := &ast.UnaryExpr{Op: token.ARROW, X: g.symbol(s.ChannelSymbolID)}
			if len(s.Bindings) == 0 {
				stmt = &ast.ExprStmt{X: recv}
			} else {
				lhs := []ast.Expr{}
				for _, binding := range s.Bindings {
					if binding == nil {
						lhs = append(lhs, ast.NewIdent("_"))
					} else {
						lhs = append(lhs, g.symbol(*binding))
					}
				}
				stmt = &ast.AssignStmt{Lhs: lhs, Tok: token.DEFINE, Rhs: []ast.Expr{recv}}
			}
		case "close":
			stmt = &ast.ExprStmt{X: &ast.CallExpr{Fun: ast.NewIdent("close"), Args: []ast.Expr{g.symbol(s.ChannelSymbolID)}}}
		case "print":
			g.usesPrint = true
			stmt = &ast.ExprStmt{X: &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("fmt"), Sel: ast.NewIdent("Println")}, Args: []ast.Expr{g.expression(s.Value)}}}
		case "return":
			stmt = &ast.ReturnStmt{}
		}
		block.List = append(block.List, stmt)
	}
	return block
}

type generatedUse struct {
	symbolID string
	nodeID   string
	ident    *ast.Ident
}
type generatedMapping struct {
	fset                        *token.FileSet
	g                           *generator
	spans                       map[string]Span
	definitions                 map[string]*ast.Ident
	uses, predeclared, packages []generatedUse
}

func (m *generatedMapping) add(id string, n ast.Node) { m.spans[id] = spanForNode(m.fset, n) }
func (m *generatedMapping) definition(id string, n ast.Expr) {
	ident := n.(*ast.Ident)
	m.definitions[id] = ident
	m.add(id, n)
}
func (m *generatedMapping) use(id, nodeID string, n ast.Expr) {
	m.uses = append(m.uses, generatedUse{id, nodeID, n.(*ast.Ident)})
}
func (m *generatedMapping) predeclaredUse(id string, n ast.Expr) {
	m.predeclared = append(m.predeclared, generatedUse{nodeID: id, ident: n.(*ast.Ident)})
}
func (m *generatedMapping) expression(e *Expr, n ast.Expr) {
	m.add(e.ID, n)
	if e.Kind == "variable" {
		m.use(e.SymbolID, e.ID, n)
	}
	if e.Kind == "bool_literal" {
		m.predeclaredUse(e.ID, n)
	}
}
func (m *generatedMapping) block(f *Function, b *ast.BlockStmt) error {
	m.add(f.Body.ID, b)
	if len(f.Body.Statements) != len(b.List) {
		return fmt.Errorf("generated statement count does not match IR")
	}
	for i, s := range f.Body.Statements {
		node := b.List[i]
		m.add(s.ID, node)
		switch s.Kind {
		case "make_channel":
			n := node.(*ast.AssignStmt)
			m.definition(s.SymbolID, n.Lhs[0])
			m.predeclaredUse(s.ID, n.Rhs[0].(*ast.CallExpr).Fun)
		case "let":
			n := node.(*ast.AssignStmt)
			m.definition(s.SymbolID, n.Lhs[0])
			m.expression(s.Value, n.Rhs[0])
		case "spawn":
			n := node.(*ast.GoStmt).Call.Fun.(*ast.FuncLit)
			m.add(s.FunctionID, n)
			if err := m.block(m.g.functions[s.FunctionID], n.Body); err != nil {
				return err
			}
		case "send":
			n := node.(*ast.SendStmt)
			m.use(s.ChannelSymbolID, s.ID, n.Chan)
			m.expression(s.Value, n.Value)
		case "receive":
			var recv *ast.UnaryExpr
			if len(s.Bindings) == 0 {
				recv = node.(*ast.ExprStmt).X.(*ast.UnaryExpr)
			} else {
				n := node.(*ast.AssignStmt)
				recv = n.Rhs[0].(*ast.UnaryExpr)
				for i, id := range s.Bindings {
					if id != nil {
						m.definition(*id, n.Lhs[i])
					}
				}
			}
			m.use(s.ChannelSymbolID, s.ID, recv.X)
		case "close":
			n := node.(*ast.ExprStmt).X.(*ast.CallExpr)
			m.use(s.ChannelSymbolID, s.ID, n.Args[0])
			m.predeclaredUse(s.ID, n.Fun)
		case "print":
			n := node.(*ast.ExprStmt).X.(*ast.CallExpr)
			m.expression(s.Value, n.Args[0])
			m.packages = append(m.packages, generatedUse{nodeID: s.ID, ident: n.Fun.(*ast.SelectorExpr).X.(*ast.Ident)})
		}
	}
	return nil
}
func smallestNode(spans map[string]Span, offset int) string {
	best := ""
	length := int(^uint(0) >> 1)
	for id, span := range spans {
		if span.StartByte <= offset && offset < span.EndByte && span.EndByte-span.StartByte < length {
			best = id
			length = span.EndByte - span.StartByte
		}
	}
	return best
}
