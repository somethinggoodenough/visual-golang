package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Import never returns a partial editable model. In particular, type errors are
// checked before lowering the supported subset, so legal but unsupported Go is
// distinguishable from an invalid source draft.
func Import(source string) Result {
	result := Result{Source: source, SourceHash: sourceDigest(source), Diagnostics: []Diagnostic{}}
	if len(source) > MaxSourceBytes {
		result.Status = "unsupported"
		result.Diagnostics = []Diagnostic{sourceDiagnostic(source, 0, "source_limit", "subset", "Source exceeds the 1 MiB product limit.")}
		return result
	}
	if !utf8.ValidString(source) {
		offset := 0
		for offset < len(source) {
			_, size := utf8.DecodeRuneInString(source[offset:])
			if size == 1 && source[offset] >= utf8.RuneSelf {
				break
			}
			offset += size
		}
		result.Status = "invalid"
		result.Diagnostics = []Diagnostic{sourceDiagnostic(source, offset, "invalid_encoding", "parse", "Source must contain valid UTF-8.")}
		return result
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", source, parser.ParseComments|parser.AllErrors)
	if err != nil {
		result.Status = "invalid"
		if errors, ok := err.(scanner.ErrorList); ok {
			for _, e := range errors {
				result.Diagnostics = append(result.Diagnostics, sourceDiagnostic(source, e.Pos.Offset, "syntax_error", "parse", e.Msg))
			}
		} else {
			result.Diagnostics = append(result.Diagnostics, sourceDiagnostic(source, 0, "syntax_error", "parse", err.Error()))
		}
		return result
	}
	unsupported := func(node ast.Node, code, message string) Result {
		span := spanForNode(fset, node)
		result.Status = "unsupported"
		result.Diagnostics = []Diagnostic{{Code: code, Severity: "error", Phase: "subset", Message: message, Span: &span}}
		return result
	}
	if file.Name.Name != "main" {
		return unsupported(file.Name, "unsupported_package", "v0 supports only package main.")
	}
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if path != "fmt" {
			return unsupported(spec, "unsupported_import", "v0 supports only the standard library fmt package; external dependencies are not loaded.")
		}
	}
	info, diagnostics := checkGoTypes(fset, file)
	if len(diagnostics) != 0 {
		result.Status = "invalid"
		result.Diagnostics = diagnostics
		return result
	}
	for _, spec := range file.Imports {
		if spec.Name != nil {
			return unsupported(spec, "unsupported_import_alias", "Import aliases, blank imports and dot imports are not supported in v0.")
		}
	}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			text := comment.Text
			if strings.HasPrefix(text, "//go:") || strings.HasPrefix(text, "//line ") || strings.HasPrefix(text, "/*line ") || strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(text, "//")), "+build ") {
				return unsupported(comment, "unsupported_directive", "Compiler directives, build tags and source position directives are not supported in v0.")
			}
		}
	}
	var main *ast.FuncDecl
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			continue
		}
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "main" || fn.Recv != nil {
			return unsupported(decl, "unsupported_declaration", "v0 supports only imports and one main function at package scope.")
		}
		main = fn
	}
	if main == nil {
		result.Status = "invalid"
		result.Diagnostics = []Diagnostic{sourceDiagnostic(source, len(source), "missing_main", "types", "package main must declare func main().")}
		return result
	}
	if !parameterless(main.Type) || main.Body == nil {
		return unsupported(main, "unsupported_main", "main must have a body, no parameters and no results.")
	}
	lower := &sourceLowerer{
		fset: fset, info: info,
		program: &Program{SchemaVersion: "1.0", PackageName: "main", Functions: []Function{}, Symbols: []Symbol{}},
		mapping: map[string]Span{}, objects: map[types.Object]string{}, owners: map[types.Object]string{},
	}
	lower.program.ID = lower.id("program", file)
	lower.program.EntryFunctionID = lower.function(main, main.Body, "main", nil)
	if len(lower.diagnostics) != 0 {
		result.Status = "unsupported"
		result.Diagnostics = lower.diagnostics
		return result
	}
	if diagnostics := Validate(lower.program); len(diagnostics) != 0 {
		// Product limits are unsupported source, rather than a Go type error.
		for i := range diagnostics {
			diagnostics[i].Phase = "subset"
			if span, ok := lower.mapping[diagnostics[i].NodeID]; ok {
				diagnostics[i].Span = &span
			} else {
				span := spanForNode(fset, file)
				diagnostics[i].Span = &span
			}
		}
		result.Status, result.Diagnostics = "unsupported", diagnostics
		return result
	}
	result.Status, result.IR, result.SourceMap = "editable", lower.program, lower.mapping
	return result
}

func sourceDigest(source string) string {
	hash := sha256.Sum256([]byte(source))
	return hex.EncodeToString(hash[:])
}

func spanForNode(fset *token.FileSet, node ast.Node) Span {
	start := fset.PositionFor(node.Pos(), false)
	end := fset.PositionFor(node.End(), false)
	return Span{File: "main.go", StartByte: start.Offset, EndByte: end.Offset, StartLine: start.Line, StartColumn: start.Column}
}

func sourceDiagnostic(source string, offset int, code, phase, message string) Diagnostic {
	offset = max(0, min(offset, len(source)))
	prefix := source[:offset]
	line := strings.Count(prefix, "\n") + 1
	column := offset - strings.LastIndex(prefix, "\n")
	end := offset
	if end < len(source) {
		_, width := utf8.DecodeRuneInString(source[end:])
		end += width
	}
	return Diagnostic{Code: code, Severity: "error", Phase: phase, Message: message, Span: &Span{File: "main.go", StartByte: offset, EndByte: end, StartLine: line, StartColumn: column}}
}

var standardImporter = struct {
	sync.Mutex
	value types.Importer
}{value: importer.Default()}

// checkGoTypes uses the actual host architecture's int size. The importer cache
// is shared and protected because conversion requests may arrive concurrently.
func checkGoTypes(fset *token.FileSet, file *ast.File) (*types.Info, []Diagnostic) {
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	diagnostics := []Diagnostic{}
	config := types.Config{
		Importer: standardImporter.value,
		Sizes:    types.SizesFor("gc", runtime.GOARCH),
		Error: func(err error) {
			d := Diagnostic{Code: "type_error", Severity: "error", Phase: "types", Message: err.Error()}
			if e, ok := err.(types.Error); ok {
				pos := fset.PositionFor(e.Pos, false)
				end := pos.Offset
				if f := fset.File(e.Pos); f != nil && end < f.Size() {
					end++
				}
				d.Message = e.Msg
				d.Span = &Span{File: "main.go", StartByte: pos.Offset, EndByte: end, StartLine: pos.Line, StartColumn: pos.Column}
			}
			if d.Span == nil {
				span := spanForNode(fset, file)
				d.Span = &span
			}
			diagnostics = append(diagnostics, d)
		},
	}
	standardImporter.Lock()
	_, _ = config.Check("main", fset, []*ast.File{file}, info)
	standardImporter.Unlock()
	return info, diagnostics
}

type sourceLowerer struct {
	fset        *token.FileSet
	info        *types.Info
	program     *Program
	mapping     map[string]Span
	objects     map[types.Object]string
	owners      map[types.Object]string
	diagnostics []Diagnostic
	sequence    int
}

func (l *sourceLowerer) id(prefix string, node ast.Node) string {
	l.sequence++
	id := fmt.Sprintf("%s_%d", prefix, l.sequence)
	l.mapping[id] = spanForNode(l.fset, node)
	return id
}

func (l *sourceLowerer) reject(node ast.Node, code, message string) {
	span := spanForNode(l.fset, node)
	l.diagnostics = append(l.diagnostics, Diagnostic{Code: code, Severity: "error", Phase: "subset", Message: message, Span: &span})
}

func parameterless(fn *ast.FuncType) bool {
	return (fn.Params == nil || len(fn.Params.List) == 0) && (fn.Results == nil || len(fn.Results.List) == 0) && (fn.TypeParams == nil || len(fn.TypeParams.List) == 0)
}

func unparen(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func (l *sourceLowerer) function(node ast.Node, body *ast.BlockStmt, kind string, parent *string) string {
	fn := Function{ID: l.id("fn", node), Kind: kind, ParentFunctionID: parent, Body: Block{ID: l.id("block", body), Statements: []Statement{}}}
	index := len(l.program.Functions)
	l.program.Functions = append(l.program.Functions, fn)
	for _, stmt := range body.List {
		if lowered := l.statement(stmt, fn.ID, fn.Body.ID); lowered != nil {
			fn.Body.Statements = append(fn.Body.Statements, *lowered)
		}
	}
	l.program.Functions[index] = fn
	return fn.ID
}

func (l *sourceLowerer) symbol(ident *ast.Ident, kind, owner, block string) string {
	object := l.info.Defs[ident]
	if object == nil || ident.Name == "_" {
		l.reject(ident, "unsupported_assignment", "Every non-blank receive or declaration binding must introduce a new variable.")
		return ""
	}
	id := l.id("sym", ident)
	l.objects[object], l.owners[object] = id, owner
	l.program.Symbols = append(l.program.Symbols, Symbol{ID: id, Name: ident.Name, Type: kind, ScopeID: block})
	return id
}

func (l *sourceLowerer) channel(expr ast.Expr) string {
	ident, ok := unparen(expr).(*ast.Ident)
	if !ok {
		l.reject(expr, "unsupported_channel_reference", "Channel operations require a direct reference to a channel variable.")
		return ""
	}
	object := l.info.Uses[ident]
	id := l.objects[object]
	if id == "" || object == nil || !types.Identical(object.Type(), types.NewChan(types.SendRecv, types.Typ[types.Int])) {
		l.reject(expr, "unsupported_channel_reference", "v0 supports references to channels created by make(chan int) only.")
		return ""
	}
	return id
}

func (l *sourceLowerer) builtin(expr ast.Expr, name string) bool {
	ident, ok := unparen(expr).(*ast.Ident)
	return ok && l.info.Uses[ident] == types.Universe.Lookup(name)
}

func (l *sourceLowerer) statement(node ast.Stmt, owner, block string) *Statement {
	stmt := &Statement{ID: l.id("stmt", node)}
	switch n := node.(type) {
	case *ast.GoStmt:
		literal, ok := unparen(n.Call.Fun).(*ast.FuncLit)
		if !ok || len(n.Call.Args) != 0 || n.Call.Ellipsis.IsValid() || !parameterless(literal.Type) {
			l.reject(node, "unsupported_spawn", "v0 supports only go func() { ... }() with no parameters or results.")
			return nil
		}
		stmt.Kind = "spawn"
		stmt.FunctionID = l.function(literal, literal.Body, "goroutine", StringPtr(owner))
	case *ast.SendStmt:
		stmt.Kind, stmt.ChannelSymbolID, stmt.Value = "send", l.channel(n.Chan), l.expression(n.Value, owner)
	case *ast.ReturnStmt:
		if len(n.Results) != 0 {
			l.reject(node, "unsupported_return", "Only return without values is supported.")
			return nil
		}
		stmt.Kind = "return"
	case *ast.AssignStmt:
		if n.Tok != token.DEFINE || len(n.Rhs) != 1 {
			l.reject(node, "unsupported_assignment", "v0 supports short declarations only; reassignment is not supported.")
			return nil
		}
		identifiers := make([]*ast.Ident, len(n.Lhs))
		for i, lhs := range n.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok || (ident.Name != "_" && l.info.Defs[ident] == nil) {
				l.reject(lhs, "unsupported_assignment", "Every non-blank binding must introduce a new variable.")
				return nil
			}
			identifiers[i] = ident
		}
		rhs := unparen(n.Rhs[0])
		if receive, ok := rhs.(*ast.UnaryExpr); ok && receive.Op == token.ARROW {
			stmt.Kind, stmt.ChannelSymbolID = "receive", l.channel(receive.X)
			stmt.Bindings = make([]*string, len(identifiers))
			for i, ident := range identifiers {
				if ident.Name == "_" {
					continue
				}
				kind := "int"
				if i == 1 {
					kind = "bool"
				}
				stmt.Bindings[i] = StringPtr(l.symbol(ident, kind, owner, block))
			}
			return stmt
		}
		if len(identifiers) != 1 || identifiers[0].Name == "_" {
			l.reject(node, "unsupported_declaration", "Local declarations must create one named int, bool or channel variable.")
			return nil
		}
		if call, ok := rhs.(*ast.CallExpr); ok && l.builtin(call.Fun, "make") {
			if len(call.Args) < 1 || len(call.Args) > 2 || call.Ellipsis.IsValid() {
				l.reject(node, "unsupported_make", "v0 supports make(chan int) or make(chan int, N).")
				return nil
			}
			channelType, ok := unparen(call.Args[0]).(*ast.ChanType)
			if !ok || channelType.Dir != ast.SEND|ast.RECV || !types.Identical(l.info.TypeOf(channelType.Value), types.Typ[types.Int]) {
				l.reject(call.Args[0], "unsupported_channel_type", "Only bidirectional chan int channels are supported.")
				return nil
			}
			stmt.Kind = "make_channel"
			if len(call.Args) == 2 {
				literal, ok := unparen(call.Args[1]).(*ast.BasicLit)
				if !ok || literal.Kind != token.INT || !decimalLiteral(literal.Value) {
					l.reject(call.Args[1], "unsupported_capacity", "Channel capacity must be a non-negative decimal integer literal.")
					return nil
				}
				capacity, exact := constant.Int64Val(l.info.Types[literal].Value)
				if !exact || capacity > int64(MaxCapacity) {
					l.reject(call.Args[1], "capacity_limit", fmt.Sprintf("Channel capacity exceeds the configured limit of %d.", MaxCapacity))
					return nil
				}
				stmt.Capacity = int(capacity)
			}
			stmt.SymbolID = l.symbol(identifiers[0], "chan int", owner, block)
			return stmt
		}
		stmt.Kind, stmt.Value = "let", l.expression(n.Rhs[0], owner)
		object := l.info.Defs[identifiers[0]]
		kind := "int"
		if object != nil && types.Identical(object.Type(), types.Typ[types.Bool]) {
			kind = "bool"
		}
		stmt.SymbolID = l.symbol(identifiers[0], kind, owner, block)
	case *ast.ExprStmt:
		expr := unparen(n.X)
		if receive, ok := expr.(*ast.UnaryExpr); ok && receive.Op == token.ARROW {
			stmt.Kind, stmt.ChannelSymbolID, stmt.Bindings = "receive", l.channel(receive.X), []*string{}
			return stmt
		}
		call, ok := expr.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || call.Ellipsis.IsValid() {
			l.reject(node, "unsupported_expression_statement", "Only channel receive, close(ch), and single-argument fmt.Println(expr) are supported.")
			return nil
		}
		if l.builtin(call.Fun, "close") {
			stmt.Kind, stmt.ChannelSymbolID = "close", l.channel(call.Args[0])
			return stmt
		}
		if selector, ok := unparen(call.Fun).(*ast.SelectorExpr); ok {
			object, ok := l.info.Uses[selector.Sel].(*types.Func)
			if ok && object.Pkg() != nil && object.Pkg().Path() == "fmt" && object.Name() == "Println" {
				stmt.Kind, stmt.Value = "print", l.expression(call.Args[0], owner)
				return stmt
			}
		}
		l.reject(node, "unsupported_call", "v0 supports only the actual built-in close and standard library fmt.Println calls.")
		return nil
	default:
		l.reject(node, "unsupported_statement", fmt.Sprintf("%T is outside the v0 language subset.", node))
		return nil
	}
	return stmt
}

func decimalLiteral(text string) bool {
	if text == "0" {
		return true
	}
	if len(text) == 0 || text[0] < '1' || text[0] > '9' {
		return false
	}
	for _, ch := range text[1:] {
		if (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}

func (l *sourceLowerer) expression(original ast.Expr, owner string) *Expr {
	expr := unparen(original)
	result := &Expr{ID: l.id("expr", original)}
	if literal, ok := expr.(*ast.BasicLit); ok && literal.Kind == token.INT && decimalLiteral(literal.Value) {
		result.Kind, result.Value = "int_literal", l.info.Types[expr].Value.ExactString()
		return result
	}
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.SUB {
		if literal, ok := unparen(unary.X).(*ast.BasicLit); ok && literal.Kind == token.INT && decimalLiteral(literal.Value) {
			result.Kind, result.Value = "int_literal", l.info.Types[expr].Value.ExactString()
			return result
		}
	}
	if ident, ok := expr.(*ast.Ident); ok {
		object := l.info.Uses[ident]
		if object == types.Universe.Lookup("true") || object == types.Universe.Lookup("false") {
			result.Kind, result.Value = "bool_literal", constant.BoolVal(l.info.Types[expr].Value)
			return result
		}
		if object != nil && (types.Identical(object.Type(), types.Typ[types.Int]) || types.Identical(object.Type(), types.Typ[types.Bool])) && l.objects[object] != "" {
			if l.owners[object] != owner {
				l.reject(original, "unsupported_capture", "A goroutine may capture outer channels only; capturing outer int or bool variables is not supported.")
				return nil
			}
			result.Kind, result.SymbolID = "variable", l.objects[object]
			return result
		}
	}
	l.reject(original, "unsupported_expression", "Expressions must be decimal integer literals, negative integer literals, boolean literals, or local int/bool variables.")
	return nil
}
