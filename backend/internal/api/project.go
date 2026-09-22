package api

import (
	"crypto/sha256"
	"encoding/hex"
	"math"

	"goviz/backend/internal/engine"
)

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Viewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}
type Layout struct {
	Nodes     map[string]Position `json:"nodes"`
	Viewport  Viewport            `json:"viewport"`
	Collapsed map[string]bool     `json:"collapsed"`
}
type Project struct {
	FileFormatVersion      string                 `json:"fileFormatVersion"`
	ProgramRevision        int64                  `json:"programRevision"`
	IR                     *engine.Program        `json:"ir"`
	Layout                 Layout                 `json:"layout"`
	Source                 string                 `json:"source"`
	SourceHash             string                 `json:"sourceHash"`
	SourceMap              map[string]engine.Span `json:"sourceMap"`
	OriginalImportedSource string                 `json:"originalImportedSource,omitempty"`
}

func validateProject(p *Project) []engine.Diagnostic {
	bad := func(code, message string) []engine.Diagnostic {
		return []engine.Diagnostic{{Code: code, Severity: "error", Phase: "ir", Message: message}}
	}
	if p == nil || p.FileFormatVersion != "1.0" {
		return bad("project_version", "不支持的项目文件版本；当前仅支持 1.0。")
	}
	if p.ProgramRevision < 0 || p.ProgramRevision > 9007199254740991 {
		return bad("project_revision", "项目修订号必须是非负安全整数。")
	}
	gen := engine.Generate(p.IR)
	if gen.Status != "ok" {
		return gen.Diagnostics
	}
	if len(p.OriginalImportedSource) > engine.MaxSourceBytes {
		return bad("project_source_size", "原始源码副本超过大小限制。")
	}
	imported := engine.Import(p.Source)
	if imported.Status != "editable" {
		return bad("project_source", "项目源码无法转换为受支持的程序。请单独导入 Go 源码查看诊断。")
	}
	if engine.Normalize(p.IR) != engine.Normalize(imported.IR) {
		return bad("project_inconsistent", "项目源码与 IR 不一致，未加载项目。")
	}
	hash := sha256.Sum256([]byte(p.Source))
	if p.SourceHash != hex.EncodeToString(hash[:]) {
		return bad("project_hash", "项目 sourceHash 与源码内容不匹配。")
	}
	validIDs := map[string]bool{}
	for _, f := range p.IR.Functions {
		validIDs[f.ID] = true
		validIDs[f.Body.ID] = true
		for _, s := range f.Body.Statements {
			validIDs[s.ID] = true
		}
	}
	for _, s := range p.IR.Symbols {
		if s.Type == "chan int" {
			validIDs[s.ID] = true
		}
	}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= 1e7 }
	for id, pos := range p.Layout.Nodes {
		if !validIDs[id] || !finite(pos.X) || !finite(pos.Y) {
			return bad("project_layout", "布局包含无效节点引用或坐标："+id)
		}
	}
	for id := range p.Layout.Collapsed {
		if !validIDs[id] {
			return bad("project_layout", "展开状态包含无效节点引用："+id)
		}
	}
	v := p.Layout.Viewport
	if !finite(v.X) || !finite(v.Y) || !finite(v.Zoom) || v.Zoom <= 0 || v.Zoom > 10 {
		return bad("project_viewport", "画布视口参数无效。")
	}
	// Maps in project files are untrusted. Rebuild against the exact saved source,
	// then align equivalent structures to preserve all existing editor IDs.
	p.SourceMap = alignSourceMap(p.IR, imported.IR, imported.SourceMap)
	p.SourceHash = imported.SourceHash
	return nil
}

func alignSourceMap(saved, parsed *engine.Program, spans map[string]engine.Span) map[string]engine.Span {
	out := map[string]engine.Span{}
	copySpan := func(a, b string) {
		if s, ok := spans[b]; ok {
			out[a] = s
		}
	}
	left, right := map[string]engine.Function{}, map[string]engine.Function{}
	for _, f := range saved.Functions {
		left[f.ID] = f
	}
	for _, f := range parsed.Functions {
		right[f.ID] = f
	}
	var walk func(string, string)
	walk = func(a, b string) {
		f, g := left[a], right[b]
		copySpan(f.ID, g.ID)
		copySpan(f.Body.ID, g.Body.ID)
		for i, s := range f.Body.Statements {
			t := g.Body.Statements[i]
			copySpan(s.ID, t.ID)
			if s.Value != nil && t.Value != nil {
				copySpan(s.Value.ID, t.Value.ID)
			}
			if s.SymbolID != "" {
				copySpan(s.SymbolID, t.SymbolID)
			}
			for j, sym := range s.Bindings {
				if sym != nil && t.Bindings[j] != nil {
					copySpan(*sym, *t.Bindings[j])
				}
			}
			if s.Kind == "spawn" {
				walk(s.FunctionID, t.FunctionID)
			}
		}
	}
	copySpan(saved.ID, parsed.ID)
	walk(saved.EntryFunctionID, parsed.EntryFunctionID)
	return out
}
