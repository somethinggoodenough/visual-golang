package analysis

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"goviz/backend/internal/engine"
)

// A bounded structural view is deliberately not a full AST or a source editor.
const MaxNodes = 10000

var standardImports = struct {
	sync.Mutex
	importer types.Importer
}{importer: importer.Default()}

type standardOnlyImporter struct{}

func (standardOnlyImporter) Import(path string) (*types.Package, error) {
	pkg, err := build.Default.Import(path, "", build.FindOnly)
	if err != nil || !pkg.Goroot {
		return nil, fmt.Errorf("only installed standard-library imports are supported: %s", path)
	}
	return standardImports.importer.Import(path)
}

func Analyze(source string) Result {
	result := Result{SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(source))), Diagnostics: []engine.Diagnostic{}}
	if len(source) > engine.MaxSourceBytes {
		result.Status = "source_size"
		result.Diagnostics = append(result.Diagnostics, engine.Diagnostic{Code: "source_limit", Severity: "error", Phase: "analysis", Message: "Source exceeds the 1 MiB limit."})
		return result
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", source, parser.AllErrors)
	if err != nil {
		result.Status = "parse_error"
		if list, ok := err.(scanner.ErrorList); ok {
			for _, e := range list {
				result.Diagnostics = append(result.Diagnostics, diagnosticAt(source, e.Pos.Offset, "parse", e.Msg))
			}
		} else {
			result.Diagnostics = append(result.Diagnostics, diagnosticAt(source, 0, "parse", err.Error()))
		}
		return result
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	config := types.Config{Importer: standardOnlyImporter{}, Sizes: types.SizesFor("gc", runtime.GOARCH), Error: func(err error) {
		if e, ok := err.(types.Error); ok {
			result.Diagnostics = append(result.Diagnostics, diagnosticAt(source, fset.PositionFor(e.Pos, false).Offset, "types", e.Msg))
		} else {
			result.Diagnostics = append(result.Diagnostics, diagnosticAt(source, 0, "types", err.Error()))
		}
	}}
	standardImports.Lock()
	_, _ = config.Check(file.Name.Name, fset, []*ast.File{file}, info)
	standardImports.Unlock()
	if len(result.Diagnostics) != 0 {
		result.Status = "type_error"
		return result
	}
	p := &projection{source: source, fset: fset, info: info, model: Program{SchemaVersion: "1.0", PackageName: file.Name.Name, Functions: []Function{}, Nodes: []Node{}, Channels: []Channel{}}, channels: map[types.Object]string{}, functions: map[types.Object]string{}}
	p.declarations(file)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name, receiver := d.Name.Name, ""
			if d.Recv != nil && len(d.Recv.List) != 0 {
				receiver = p.text(d.Recv.List[0].Type)
				name = receiver + "." + name
			}
			p.function(d, d.Body, name, receiver, "", "")
		case *ast.GenDecl:
			if d.Tok == token.VAR || d.Tok == token.CONST {
				p.declaration(d, "", "")
			}
		}
	}
	if p.limited {
		result.Status = "analysis_limit"
		result.Diagnostics = append(result.Diagnostics, engine.Diagnostic{Code: "analysis_limit", Severity: "error", Phase: "analysis", Message: fmt.Sprintf("Structural analysis exceeds the %d node limit.", MaxNodes)})
		return result
	}
	result.Status, result.Model = "ok", &p.model
	return result
}

func diagnosticAt(source string, offset int, phase, message string) engine.Diagnostic {
	offset = max(0, min(offset, len(source)))
	end := offset
	if offset < len(source) {
		_, size := utf8.DecodeRuneInString(source[offset:])
		end += size
	}
	prefix := source[:offset]
	return engine.Diagnostic{Code: phase + "_error", Severity: "error", Phase: phase, Message: message, Span: &engine.Span{File: "main.go", StartByte: offset, EndByte: end, StartLine: strings.Count(prefix, "\n") + 1, StartColumn: offset - strings.LastIndex(prefix, "\n")}}
}

type projection struct {
	source    string
	fset      *token.FileSet
	info      *types.Info
	model     Program
	channels  map[types.Object]string
	functions map[types.Object]string
	limited   bool
}

func (p *projection) span(n ast.Node) engine.Span {
	start, end := p.fset.PositionFor(n.Pos(), false), p.fset.PositionFor(n.End(), false)
	return engine.Span{File: "main.go", StartByte: start.Offset, EndByte: end.Offset, StartLine: start.Line, StartColumn: start.Column}
}

func (p *projection) id(kind string, n ast.Node) string {
	s := p.span(n)
	return fmt.Sprintf("%s_%d_%d", kind, s.StartByte, s.EndByte)
}

func (p *projection) text(n ast.Node) string {
	if n == nil {
		return ""
	}
	s := p.span(n)
	return p.source[s.StartByte:s.EndByte]
}

func (p *projection) add(kind string, n ast.Node, parent, fn string) *Node {
	span := p.span(n)
	label := strings.TrimSpace(strings.Split(p.text(n), "\n")[0])
	runes := []rune(label)
	if len(runes) > 140 {
		label = string(runes[:137]) + "..."
	}
	node := Node{ID: p.id(kind, n), Kind: kind, Label: label, ParentID: parent, FunctionID: fn, Span: span}
	if len(p.model.Nodes) >= MaxNodes {
		p.limited = true
		return &node
	}
	p.model.Nodes = append(p.model.Nodes, node)
	return &p.model.Nodes[len(p.model.Nodes)-1]
}

func (p *projection) declarations(file *ast.File) {
	parameters := map[*ast.Ident]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if f, ok := n.(*ast.FuncType); ok {
			for _, list := range []*ast.FieldList{f.Params, f.Results} {
				if list != nil {
					for _, field := range list.List {
						for _, ident := range field.Names {
							parameters[ident] = true
						}
					}
				}
			}
		}
		return true
	})
	idents := []*ast.Ident{}
	for ident, object := range p.info.Defs {
		if object != nil && ident.Name != "_" {
			idents = append(idents, ident)
		}
	}
	sort.Slice(idents, func(i, j int) bool { return idents[i].Pos() < idents[j].Pos() })
	for _, ident := range idents {
		object := p.info.Defs[ident]
		variable, ok := object.(*types.Var)
		if !ok {
			continue
		}
		if _, ok := variable.Type().Underlying().(*types.Chan); !ok {
			continue
		}
		kind := "local"
		if variable.IsField() {
			kind = "field"
		} else if parameters[ident] {
			kind = "parameter"
		} else if object.Pkg() != nil && object.Parent() == object.Pkg().Scope() {
			kind = "global"
		}
		id := p.id("channel", ident)
		p.channels[object] = id
		p.model.Channels = append(p.model.Channels, Channel{ID: id, Name: ident.Name, Type: types.TypeString(variable.Type(), nil), Kind: kind, Span: p.span(ident)})
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			p.functions[p.info.Defs[fn.Name]] = p.id("function", fn)
		}
	}
}

func (p *projection) function(n ast.Node, body *ast.BlockStmt, name, receiver, parentFn, parentNode string) string {
	id := p.id("function", n)
	p.model.Functions = append(p.model.Functions, Function{ID: id, Name: name, Receiver: receiver, ParentFunctionID: parentFn, Span: p.span(n)})
	node := p.add("function", n, parentNode, id)
	node.Label = name
	if body != nil {
		p.statement(body, id, id)
	}
	return id
}

func (p *projection) object(expr ast.Expr) types.Object {
	switch e := expr.(type) {
	case *ast.Ident:
		if obj := p.info.Uses[e]; obj != nil {
			return obj
		}
		return p.info.Defs[e]
	case *ast.SelectorExpr:
		if selection := p.info.Selections[e]; selection != nil {
			return selection.Obj()
		}
		return p.info.Uses[e.Sel]
	case *ast.ParenExpr:
		return p.object(e.X)
	}
	return nil
}

func (p *projection) channel(expr ast.Expr) string { return p.channels[p.object(expr)] }

func (p *projection) declaration(decl *ast.GenDecl, parent, fn string) {
	for _, spec := range decl.Specs {
		if value, ok := spec.(*ast.ValueSpec); ok {
			node := p.add("declare", value, parent, fn)
			for i, expr := range value.Values {
				channel := ""
				if i < len(value.Names) {
					channel = p.channel(value.Names[i])
				}
				p.expression(expr, node.ID, fn, channel)
			}
		}
	}
}

func (p *projection) statement(stmt ast.Stmt, parent, fn string) {
	if stmt == nil || p.limited {
		return
	}
	switch s := stmt.(type) {
	case *ast.BlockStmt:
		node := p.add("block", s, parent, fn)
		node.Label = "{ ... }"
		for _, child := range s.List {
			p.statement(child, node.ID, fn)
		}
	case *ast.GoStmt:
		node := p.add("go", s, parent, fn)
		node.TargetFunctionID = p.functions[p.object(s.Call.Fun)]
		if literal, ok := s.Call.Fun.(*ast.FuncLit); ok {
			node.TargetFunctionID = p.id("function", literal)
		}
		p.expression(s.Call, node.ID, fn, "")
	case *ast.DeferStmt:
		node := p.add("defer", s, parent, fn)
		p.expression(s.Call, node.ID, fn, "")
	case *ast.SendStmt:
		node := p.add("send", s, parent, fn)
		node.ChannelID = p.channel(s.Chan)
		p.expression(s.Chan, node.ID, fn, "")
		p.expression(s.Value, node.ID, fn, "")
	case *ast.AssignStmt:
		node := p.add("assign", s, parent, fn)
		for _, lhs := range s.Lhs {
			p.assignmentTarget(lhs, node.ID, fn, s.Tok != token.ASSIGN && s.Tok != token.DEFINE)
		}
		for i, rhs := range s.Rhs {
			channel := ""
			if i < len(s.Lhs) {
				channel = p.channel(s.Lhs[i])
			}
			p.expression(rhs, node.ID, fn, channel)
		}
	case *ast.IncDecStmt:
		node := p.add("assign", s, parent, fn)
		p.assignmentTarget(s.X, node.ID, fn, true)
	case *ast.DeclStmt:
		if decl, ok := s.Decl.(*ast.GenDecl); ok {
			p.declaration(decl, parent, fn)
		}
	case *ast.ExprStmt:
		p.expression(s.X, parent, fn, "")
	case *ast.ForStmt:
		node := p.add("for", s, parent, fn)
		p.statement(s.Init, node.ID, fn)
		p.expression(s.Cond, node.ID, fn, "")
		p.statement(s.Post, node.ID, fn)
		p.statement(s.Body, node.ID, fn)
	case *ast.RangeStmt:
		node := p.add("for", s, parent, fn)
		expressionParent := node.ID
		if typ := p.info.TypeOf(s.X); typ != nil {
			if _, ok := typ.Underlying().(*types.Chan); ok {
				node.ChannelID = p.channel(s.X)
				receive := p.add("receive", s.X, node.ID, fn)
				receive.ChannelID = node.ChannelID
				receive.Label = "range " + p.text(s.X)
				expressionParent = receive.ID
			}
		}
		p.expression(s.X, expressionParent, fn, "")
		p.statement(s.Body, node.ID, fn)
	case *ast.IfStmt:
		node := p.add("if", s, parent, fn)
		p.statement(s.Init, node.ID, fn)
		p.expression(s.Cond, node.ID, fn, "")
		p.statement(s.Body, node.ID, fn)
		p.statement(s.Else, node.ID, fn)
	case *ast.SwitchStmt:
		node := p.add("switch", s, parent, fn)
		p.statement(s.Init, node.ID, fn)
		p.expression(s.Tag, node.ID, fn, "")
		p.statement(s.Body, node.ID, fn)
	case *ast.TypeSwitchStmt:
		node := p.add("switch", s, parent, fn)
		p.statement(s.Init, node.ID, fn)
		p.statement(s.Assign, node.ID, fn)
		p.statement(s.Body, node.ID, fn)
	case *ast.SelectStmt:
		node := p.add("select", s, parent, fn)
		p.statement(s.Body, node.ID, fn)
	case *ast.CaseClause:
		node := p.add("switch_case", s, parent, fn)
		for _, expr := range s.List {
			p.expression(expr, node.ID, fn, "")
		}
		for _, stmt := range s.Body {
			p.statement(stmt, node.ID, fn)
		}
	case *ast.CommClause:
		node := p.add("select_case", s, parent, fn)
		p.statement(s.Comm, node.ID, fn)
		for _, stmt := range s.Body {
			p.statement(stmt, node.ID, fn)
		}
	case *ast.ReturnStmt:
		node := p.add("return", s, parent, fn)
		for _, expr := range s.Results {
			p.expression(expr, node.ID, fn, "")
		}
	case *ast.BranchStmt:
		p.add(s.Tok.String(), s, parent, fn)
	case *ast.LabeledStmt:
		node := p.add("label", s, parent, fn)
		p.statement(s.Stmt, node.ID, fn)
	case *ast.EmptyStmt:
	default:
		node := p.add("opaque", s, parent, fn)
		node.Opaque = true
	}
}

func (p *projection) assignmentTarget(expr ast.Expr, parent, fn string, readModify bool) {
	if paren, ok := expr.(*ast.ParenExpr); ok {
		p.assignmentTarget(paren.X, parent, fn, readModify)
		return
	}
	if index, ok := expr.(*ast.IndexExpr); ok {
		if typ := p.info.TypeOf(index.X); typ != nil {
			if _, ok := typ.Underlying().(*types.Map); ok {
				if readModify {
					p.add("map_read", index, parent, fn)
				}
				node := p.add("map_write", index, parent, fn)
				p.expression(index.X, node.ID, fn, "")
				p.expression(index.Index, node.ID, fn, "")
				return
			}
		}
	}
	p.expression(expr, parent, fn, "")
}

func (p *projection) expression(expr ast.Expr, parent, fn, assignedChannel string) {
	if expr == nil || p.limited {
		return
	}
	switch e := expr.(type) {
	case *ast.FuncLit:
		p.function(e, e.Body, fmt.Sprintf("func @%d", p.span(e).StartLine), "", fn, parent)
	case *ast.UnaryExpr:
		if e.Op == token.ARROW {
			node := p.add("receive", e, parent, fn)
			node.ChannelID = p.channel(e.X)
			parent = node.ID
		}
		p.expression(e.X, parent, fn, "")
	case *ast.CallExpr:
		kind := "call"
		if builtin, ok := p.object(e.Fun).(*types.Builtin); ok {
			switch builtin.Name() {
			case "make":
				if typ := p.info.TypeOf(e); typ != nil {
					if _, ok := typ.Underlying().(*types.Chan); ok {
						kind = "make_channel"
					}
				}
			case "close":
				kind = "close"
			}
		}
		node := p.add(kind, e, parent, fn)
		if kind == "call" {
			node.TargetFunctionID = p.functions[p.object(e.Fun)]
			if literal, ok := e.Fun.(*ast.FuncLit); ok {
				node.TargetFunctionID = p.id("function", literal)
			}
			node.Opaque = node.TargetFunctionID == ""
		} else if kind == "make_channel" {
			node.ChannelID = assignedChannel
		} else if len(e.Args) != 0 {
			node.ChannelID = p.channel(e.Args[0])
		}
		p.expression(e.Fun, node.ID, fn, "")
		for _, arg := range e.Args {
			p.expression(arg, node.ID, fn, "")
		}
	case *ast.IndexExpr:
		kind := "index_access"
		if typ := p.info.TypeOf(e.X); typ != nil {
			if _, ok := typ.Underlying().(*types.Map); ok {
				kind = "map_read"
			}
		}
		node := p.add(kind, e, parent, fn)
		p.expression(e.X, node.ID, fn, "")
		p.expression(e.Index, node.ID, fn, "")
	case *ast.SelectorExpr:
		if selection := p.info.Selections[e]; selection != nil && selection.Kind() == types.FieldVal {
			node := p.add("field_access", e, parent, fn)
			node.ChannelID = p.channel(e)
			parent = node.ID
		}
		p.expression(e.X, parent, fn, "")
	case *ast.CompositeLit:
		// Named struct fields preserve channel-declaration references even when
		// the channel is created inside a request or constructor literal.
		var fields *types.Struct
		if typ := p.info.TypeOf(e); typ != nil {
			fields, _ = typ.Underlying().(*types.Struct)
		}
		for i, elt := range e.Elts {
			channel := ""
			value := elt
			if pair, ok := elt.(*ast.KeyValueExpr); ok {
				value = pair.Value
				if fields != nil {
					if ident, ok := pair.Key.(*ast.Ident); ok {
						for j := 0; j < fields.NumFields(); j++ {
							if fields.Field(j).Name() == ident.Name {
								channel = p.channels[fields.Field(j)]
							}
						}
					}
				} else {
					p.expression(pair.Key, parent, fn, "")
				}
			} else if fields != nil && i < fields.NumFields() {
				channel = p.channels[fields.Field(i)]
			}
			p.expression(value, parent, fn, channel)
		}
	default:
		// Traverse expression operands without visualizing punctuation, types,
		// identifiers, and constants as individual blocks.
		ast.Inspect(expr, func(n ast.Node) bool {
			if n == expr {
				return true
			}
			if child, ok := n.(ast.Expr); ok {
				p.expression(child, parent, fn, assignedChannel)
				return false
			}
			return true
		})
	}
}
