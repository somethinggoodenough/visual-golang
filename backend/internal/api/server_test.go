package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goviz/backend/internal/compiler"
	"goviz/backend/internal/engine"
)

const demo = `package main
import "fmt"
// 中文注释验证 UTF-8 源码位置。
func main() {
 ch := make(chan int)
 go func() { ch <- 42 }()
 value := <-ch
 fmt.Println(value)
}`

func post(t *testing.T, h http.Handler, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080"+path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func handler() http.Handler { return (&Server{Compiler: compiler.New(30 * time.Second)}).Handler() }

func TestAPIImportGenerateBuild(t *testing.T) {
	h := handler()
	w := post(t, h, "/api/import", map[string]any{"requestId": "import-1", "source": demo})
	var imported conversionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || imported.Status != "editable" || imported.RequestID != "import-1" {
		t.Fatalf("import %d %s", w.Code, w.Body.String())
	}
	for i := range imported.IR.Functions {
		for j := range imported.IR.Functions[i].Body.Statements {
			s := &imported.IR.Functions[i].Body.Statements[j]
			if s.Kind == "send" {
				s.Value.Value = "100"
			}
		}
	}
	w = post(t, h, "/api/generate", map[string]any{"requestId": "generate-1", "programRevision": 7, "ir": imported.IR})
	var generated conversionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &generated); err != nil {
		t.Fatal(err)
	}
	if generated.Status != "ok" || !strings.Contains(generated.Source, "<- 100") || generated.ProgramRevision != 7 {
		t.Fatal(w.Body.String())
	}
	round := engine.Import(generated.Source)
	if round.Status != "editable" || engine.Normalize(round.IR) != engine.Normalize(imported.IR) {
		t.Fatalf("roundtrip: %+v", round)
	}
	w = post(t, h, "/api/build", map[string]any{"requestId": "build-1", "programRevision": 7, "ir": imported.IR})
	var built buildResponse
	_ = json.Unmarshal(w.Body.Bytes(), &built)
	if built.Status != "ok" || built.ProgramRevision != 7 || built.RequestID != "build-1" {
		t.Fatal(w.Body.String())
	}
}

func TestRequestBoundaries(t *testing.T) {
	h := handler()
	for _, tt := range []struct {
		name, body, contentType, origin, host string
		code                                  int
	}{
		{"syntax", "{", "application/json", "", "127.0.0.1:8080", 400},
		{"unknown_field", `{"requestId":"x","source":"","path":"/etc/passwd"}`, "application/json", "", "127.0.0.1", 400},
		{"missing_id", `{"source":""}`, "application/json", "", "127.0.0.1", 400},
		{"wrong_type", `{"requestId":"x","source":4}`, "application/json", "", "127.0.0.1", 400},
		{"trailing_json", `{"requestId":"x","source":""} {}`, "application/json", "", "127.0.0.1", 400},
		{"form", `requestId=x`, "text/plain", "", "127.0.0.1", 415},
		{"origin", `{"requestId":"x"}`, "application/json", "https://unrelated.example", "127.0.0.1", 403},
		{"rebind", `{"requestId":"x"}`, "application/json", "", "unrelated.example", 403},
		{"source_limit", `{"requestId":"x","source":"` + strings.Repeat("a", engine.MaxSourceBytes+1) + `"}`, "application/json", "", "127.0.0.1", 413},
		{"body_limit", `{"requestId":"x","source":"` + strings.Repeat("a", maxRequestBytes) + `"}`, "application/json", "", "127.0.0.1", 413},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://"+tt.host+"/api/import", strings.NewReader(tt.body))
			r.Header.Set("Content-Type", tt.contentType)
			r.Header.Set("Origin", tt.origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func projectFixture(t *testing.T) *Project {
	t.Helper()
	r := engine.Import(demo)
	if r.Status != "editable" {
		t.Fatal(r.Diagnostics)
	}
	return &Project{FileFormatVersion: "1.0", ProgramRevision: 3, IR: r.IR, Source: demo, SourceHash: r.SourceHash, SourceMap: map[string]engine.Span{"forged": {File: "main.go", StartByte: 9999}}, Layout: Layout{Nodes: map[string]Position{r.IR.EntryFunctionID: {X: 34, Y: 85}}, Viewport: Viewport{Zoom: 1}, Collapsed: map[string]bool{}}, OriginalImportedSource: demo}
}

func TestProjectConsistencyAndMapRebuilding(t *testing.T) {
	p := projectFixture(t)
	// Rename all IDs in the saved project, while retaining the exact source.
	b, _ := json.Marshal(p.IR)
	raw := string(b)
	ids := []string{p.IR.ID}
	for _, f := range p.IR.Functions {
		ids = append(ids, f.ID, f.Body.ID)
		for _, s := range f.Body.Statements {
			ids = append(ids, s.ID)
			if s.Value != nil {
				ids = append(ids, s.Value.ID)
			}
		}
	}
	for _, s := range p.IR.Symbols {
		ids = append(ids, s.ID)
	}
	for _, id := range ids {
		raw = strings.ReplaceAll(raw, `"`+id+`"`, `"saved_`+id+`"`)
	}
	if err := json.Unmarshal([]byte(raw), p.IR); err != nil {
		t.Fatal(err)
	}
	p.Layout.Nodes = map[string]Position{p.IR.EntryFunctionID: {X: 34, Y: 85}}
	if ds := validateProject(p); len(ds) > 0 {
		t.Fatal(ds)
	}
	if _, ok := p.SourceMap["forged"]; ok {
		t.Fatal("trusted stale map")
	}
	if _, ok := p.SourceMap[p.IR.EntryFunctionID]; !ok {
		t.Fatal("saved IDs not mapped")
	}
	if p.Layout.Nodes[p.IR.EntryFunctionID].X != 34 {
		t.Fatal("lost layout")
	}
	for _, tt := range []struct {
		name   string
		mutate func(*Project)
	}{
		{"version", func(p *Project) { p.FileFormatVersion = "2.0" }},
		{"source_model", func(p *Project) { p.Source = strings.Replace(p.Source, "42", "99", 1) }},
		{"hash", func(p *Project) { p.SourceHash = "forged" }},
		{"layout", func(p *Project) { p.Layout.Nodes["nonexistent"] = Position{} }},
		{"zoom", func(p *Project) { p.Layout.Viewport.Zoom = 0 }},
		{"revision", func(p *Project) { p.ProgramRevision = -1 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := projectFixture(t)
			tt.mutate(p)
			if ds := validateProject(p); len(ds) == 0 {
				t.Fatal("accepted corrupt project")
			}
		})
	}
}

func TestAPIVersionsAndProgramDiagnostics(t *testing.T) {
	h := handler()
	p := projectFixture(t)
	p.IR.SchemaVersion = "2.0"
	w := post(t, h, "/api/generate", map[string]any{"requestId": "version", "ir": p.IR})
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"requestId":"version"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, source := range []string{"package main; func main(){ doesNotExist() }", "package main; func main(){ for {} }"} {
		w := post(t, h, "/api/import", map[string]any{"requestId": "diagnostic", "source": source})
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"diagnostics":[{`) {
			t.Fatal(w.Body.String())
		}
	}
}

func TestResourceLayoutIDsCannotCollideWithStatements(t *testing.T) {
	p := projectFixture(t)
	var symbolID string
	for _, sym := range p.IR.Symbols {
		if sym.Type == "chan int" {
			symbolID = sym.ID
			break
		}
	}
	declarationID := "resource:" + symbolID
	p.IR.Functions[0].Body.Statements[0].ID = declarationID
	p.Layout.Nodes[symbolID] = Position{X: 40, Y: 600}
	p.Layout.Nodes[declarationID] = Position{X: 20, Y: 80}
	if ds := validateProject(p); len(ds) > 0 {
		t.Fatal(ds)
	}
	if p.Layout.Nodes[symbolID].Y == p.Layout.Nodes[declarationID].Y {
		t.Fatal("resource and declaration positions collided")
	}
}
