package trace

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Instrument changes only a temporary execution copy. Insertions contain no
// newlines, so original source line numbers survive instrumentation. Annotations
// describe statement boundaries, not operand evaluation order or channel pairing.
func Instrument(source, tracePath string, limit int, timeout time.Duration) (string, string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", source, parser.AllErrors)
	if err != nil {
		return "", "", err
	}
	if f.Name.Name != "main" {
		return "", "", fmt.Errorf("execution requires package main")
	}
	prefix := "__goviz"
	for strings.Contains(source, prefix) {
		prefix += "x"
	}
	type insertion struct {
		offset int
		text   string
	}
	var edits []insertion
	add := func(pos token.Pos, text string) { edits = append(edits, insertion{fset.Position(pos).Offset, text}) }
	mark := func(kind string, pos token.Pos) string {
		return fmt.Sprintf("%sLog(%q,%d,%d); ", prefix, kind, fset.Position(pos).Line, fset.Position(pos).Offset)
	}
	mainFound := false
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "main" && fn.Body != nil {
			mainFound = true
			add(fn.Body.Lbrace+1, " "+prefix+"Start(); defer "+prefix+"Stop(); ")
		}
	}
	if !mainFound {
		return "", "", fmt.Errorf("execution requires func main()")
	}
	annotate := func(list []ast.Stmt) {
		for _, stmt := range list {
			// A label is a control-flow target; a prefix outside it would not be
			// visited after goto. Keep its body source-mapped by runtime stacks.
			if _, ok := stmt.(*ast.LabeledStmt); ok {
				continue
			}
			if _, ok := stmt.(*ast.EmptyStmt); ok {
				continue
			}
			if _, ok := stmt.(*ast.CommClause); ok {
				continue
			}
			if _, ok := stmt.(*ast.CaseClause); ok {
				continue
			}
			before, after := "STATEMENT", ""
			switch s := stmt.(type) {
			case *ast.SendStmt:
				before, after = "SEND_ATTEMPT", "SEND_COMPLETE"
			case *ast.SelectStmt:
				before = "SELECT_ENTER"
			case *ast.GoStmt:
				before = "GO_LAUNCH"
			case *ast.ReturnStmt:
				before = "RETURN"
			case *ast.AssignStmt:
				for _, expr := range s.Rhs {
					if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.ARROW {
						before, after = "RECEIVE_ATTEMPT", "RECEIVE_COMPLETE"
					}
					if call, ok := expr.(*ast.CallExpr); ok {
						if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "make" && id.Obj == nil && len(call.Args) > 0 {
							if _, ok := call.Args[0].(*ast.ChanType); ok {
								after = "CHANNEL_CREATE"
							}
						}
					}
				}
			case *ast.ExprStmt:
				if unary, ok := s.X.(*ast.UnaryExpr); ok && unary.Op == token.ARROW {
					before, after = "RECEIVE_ATTEMPT", "RECEIVE_COMPLETE"
				}
				if call, ok := s.X.(*ast.CallExpr); ok {
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "close" && id.Obj == nil {
						before, after = "CHANNEL_CLOSE_ATTEMPT", "CHANNEL_CLOSE"
					}
				}
			}
			add(stmt.Pos(), mark(before, stmt.Pos()))
			if after != "" {
				add(stmt.End(), "; "+mark(after, stmt.Pos()))
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BlockStmt:
			annotate(n.List)
		case *ast.CommClause:
			kind := "SELECT_CASE_CHOSEN"
			add(n.Colon+1, " "+mark(kind, n.Pos()))
			// A select case body begins only after its communication completed.
			// Do not invent attempts for unchosen cases or channel identities.
			if n.Comm != nil {
				completed := "RECEIVE_COMPLETE"
				if _, ok := n.Comm.(*ast.SendStmt); ok {
					completed = "SEND_COMPLETE"
				}
				add(n.Colon+1, " "+mark(completed, n.Comm.Pos()))
			}
			annotate(n.Body)
		case *ast.CaseClause:
			annotate(n.Body)
		}
		return true
	})
	// Stable ordering keeps Start before the first statement's log when their
	// offsets coincide. Apply by walking forwards so ties retain insertion order.
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].offset < edits[j].offset })
	var b strings.Builder
	last := 0
	for _, edit := range edits {
		b.WriteString(source[last:edit.offset])
		b.WriteString(edit.text)
		last = edit.offset
	}
	b.WriteString(source[last:])
	// The timer flushes a trace at the execution deadline. The parent enforces
	// the same deadline with a short, bounded flush grace period on traced runs.
	// Its marker distinguishes our deadline exit from a user's os.Exit(124).
	helper := fmt.Sprintf(`package main
import (%[1]scontext "context"; %[1]sos "os"; %[1]strace "runtime/trace"; %[1]sstrconv "strconv"; %[1]ssync "sync"; %[1]stime "time")
var %[1]sFile *%[1]sos.File
var %[1]sOnce %[1]ssync.Once
type %[1]sWriter struct { n int }
func (w *%[1]sWriter) Write(p []byte) (int,error) {
 n:=len(p); left:=%[3]d-w.n
 if left <= 0 { _=%[1]sos.WriteFile(%[2]s+".truncated",[]byte("1"),0600); return n,nil }
 if len(p)>left { p=p[:left]; _=%[1]sos.WriteFile(%[2]s+".truncated",[]byte("1"),0600) }
 k,err:=%[1]sFile.Write(p); w.n+=k; return n,err
}
func %[1]sStart() {
 var err error
 %[1]sFile,err=%[1]sos.Create(%[2]s); if err!=nil { panic(err) }
 if err=%[1]strace.Start(&%[1]sWriter{}); err!=nil { panic(err) }
 %[1]stime.AfterFunc(%[1]stime.Duration(%[4]d),func(){
  _=%[1]sos.WriteFile(%[2]s+".timeout",[]byte("1"),0600)
  %[1]sStop()
  %[1]sos.Exit(124)
 })
}
func %[1]sStop() { %[1]sOnce.Do(func(){%[1]strace.Stop(); if %[1]sFile!=nil { _=%[1]sFile.Close() }}) }
func %[1]sLog(kind string,line,offset int) { %[1]strace.Log(%[1]scontext.Background(),"goviz",kind+":"+%[1]sstrconv.Itoa(line)+":"+%[1]sstrconv.Itoa(offset)) }
`, prefix, strconv.Quote(tracePath), limit, int64(timeout))
	return b.String(), helper, nil
}
