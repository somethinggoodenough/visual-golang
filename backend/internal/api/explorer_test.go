package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"goviz/backend/internal/analysis"
	"goviz/backend/internal/compiler"
	"goviz/backend/internal/engine"
	"goviz/backend/internal/runner"
)

type explorerAnalysisResponse struct {
	RequestID       string `json:"requestId"`
	ProgramRevision int64  `json:"programRevision"`
	analysis.Result
}

type explorerExecutionResponse struct {
	RequestID       string `json:"requestId"`
	ProgramRevision int64  `json:"programRevision"`
	runner.Result
}

func explorerHandler(t *testing.T) http.Handler {
	t.Helper()
	r := runner.New(60*time.Second, 5*time.Second)
	if r.GoPath == "" {
		t.Skip("Go toolchain unavailable")
	}
	return (&Server{Compiler: compiler.New(60 * time.Second), Runner: r}).Handler()
}

func decodeExplorerExecution(t *testing.T, w *httptest.ResponseRecorder) explorerExecutionResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var result explorerExecutionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestExplorerKVStoreAnalyzeCompileRun(t *testing.T) {
	data, err := os.ReadFile("../../../examples/kvstore.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	expectedHash := fmt.Sprintf("%x", sha256.Sum256(data))
	h := explorerHandler(t)
	w := post(t, h, "/api/analyze", map[string]any{"requestId": "kv-analysis", "programRevision": 17, "source": source})
	var analyzed explorerAnalysisResponse
	if err := json.Unmarshal(w.Body.Bytes(), &analyzed); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || analyzed.Status != "ok" || analyzed.Model == nil {
		t.Fatalf("analyze HTTP %d: %s", w.Code, w.Body.String())
	}
	if analyzed.RequestID != "kv-analysis" || analyzed.ProgramRevision != 17 || analyzed.SourceHash != expectedHash {
		t.Errorf("analysis lost request identity: %+v", analyzed)
	}
	kinds := map[string]bool{}
	for _, node := range analyzed.Model.Nodes {
		kinds[node.Kind] = true
	}
	for _, kind := range []string{"go", "for", "select", "switch", "send", "receive", "map_read", "map_write"} {
		if !kinds[kind] {
			t.Errorf("extended structure is missing %s", kind)
		}
	}
	w = post(t, h, "/api/build-source", map[string]any{"requestId": "kv-build", "programRevision": 17, "source": source})
	built := decodeExplorerExecution(t, w)
	if built.Status != "ok" || built.RequestID != "kv-build" || built.ProgramRevision != 17 || built.SourceHash != expectedHash {
		t.Fatalf("build-source: %+v", built)
	}
	if built.Stdout != "" || built.Stderr != "" || built.ExitCode != nil || built.Trace != nil {
		t.Fatalf("compile-only endpoint executed the program: %+v", built)
	}
	w = post(t, h, "/api/run", map[string]any{"requestId": "kv-run", "programRevision": 17, "source": source})
	ran := decodeExplorerExecution(t, w)
	if ran.Status != "ok" || ran.RequestID != "kv-run" || ran.ProgramRevision != 17 || ran.SourceHash != expectedHash {
		t.Fatalf("run: %+v", ran)
	}
	expectedOutput := "disk: greeting = Hello\nAPPEND: Hello\nretry: Hello\ndisk: greeting = Hello Go\nAPPEND: Hello Go\nGET: Hello Go\nstopped\n"
	if ran.Stdout != expectedOutput || ran.Stderr != "" || ran.ExitCode == nil || *ran.ExitCode != 0 || ran.OutputTruncated {
		t.Fatalf("KVStore run did not complete correctly: %+v", ran)
	}
	if ran.Trace != nil || len(ran.NativeTrace) != 0 {
		t.Fatal("ordinary Run unexpectedly enabled tracing")
	}
}

func TestExplorerRunIRFallbackAndInputConflict(t *testing.T) {
	h := explorerHandler(t)
	imported := engine.Import(demo)
	if imported.Status != "editable" {
		t.Fatal(imported.Diagnostics)
	}
	generated := engine.Generate(imported.IR)
	w := post(t, h, "/api/run", map[string]any{"requestId": "ir-run", "programRevision": 9, "ir": imported.IR})
	result := decodeExplorerExecution(t, w)
	if result.Status != "ok" || result.Stdout != "42\n" || result.RequestID != "ir-run" || result.ProgramRevision != 9 || result.SourceHash != generated.SourceHash {
		t.Fatalf("IR fallback: %+v", result)
	}
	for _, endpoint := range []string{"/api/run", "/api/build-source"} {
		w = post(t, h, endpoint, map[string]any{"requestId": "conflict", "source": demo, "ir": imported.IR})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"source_conflict"`) {
			t.Errorf("%s must reject source + IR ambiguity: %d %s", endpoint, w.Code, w.Body.String())
		}
	}
}

func TestExplorerFailedCompilationNeverExecutes(t *testing.T) {
	h := explorerHandler(t)
	source := `package main
import "fmt"
func init() { fmt.Println("must not execute init") }
func main() { fmt.Println("must not execute main"); missingFunction() }`
	w := post(t, h, "/api/run", map[string]any{"requestId": "bad-run", "programRevision": 5, "source": source})
	result := decodeExplorerExecution(t, w)
	if result.Status != "compile_error" || result.Stdout != "" || result.ExitCode != nil || result.Trace != nil || len(result.Diagnostics) == 0 {
		t.Fatalf("compile failure was not isolated from execution: %+v", result)
	}
	if result.RequestID != "bad-run" || result.ProgramRevision != 5 || result.SourceHash != fmt.Sprintf("%x", sha256.Sum256([]byte(source))) {
		t.Fatalf("compile error lost source identity: %+v", result)
	}
}

func TestExplorerTraceMapsToSameSourceSnapshot(t *testing.T) {
	h := explorerHandler(t)
	analyzed := analysis.Analyze(demo)
	if analyzed.Model == nil {
		t.Fatal(analyzed.Diagnostics)
	}
	w := post(t, h, "/api/run", map[string]any{"requestId": "trace-run", "programRevision": 23, "source": demo, "trace": true})
	result := decodeExplorerExecution(t, w)
	if result.Status != "ok" || result.Stdout != "42\n" || result.Trace == nil || result.TraceWarning != "" || len(result.NativeTrace) == 0 {
		t.Fatalf("trace capture: status=%s stdout=%q stderr=%q warning=%q diagnostics=%+v trace=%+v", result.Status, result.Stdout, result.Stderr, result.TraceWarning, result.Diagnostics, result.Trace)
	}
	if result.RequestID != "trace-run" || result.ProgramRevision != 23 || result.SourceHash != analyzed.SourceHash {
		t.Fatalf("trace was not attached to its source snapshot: %+v", result)
	}
	if result.Trace.SchemaVersion != "1.0" || len(result.Trace.Goroutines) < 2 || len(result.Trace.Events) == 0 {
		t.Fatalf("missing real execution data: %+v", result.Trace)
	}
	nodes := map[string]analysis.Node{}
	receiveNodeID, receiveStatementID := "", ""
	for _, node := range analyzed.Model.Nodes {
		nodes[node.ID] = node
		text := strings.TrimSpace(demo[node.Span.StartByte:node.Span.EndByte])
		if node.Kind == "receive" && text == "<-ch" {
			receiveNodeID, receiveStatementID = node.ID, node.ParentID
		}
	}
	if receiveNodeID == "" || nodes[receiveStatementID].Kind != "assign" {
		t.Fatal("expected receive operation nested in its assignment")
	}
	receiveCompleted, stateTransition := false, false
	receiveEventNodes := []string{}
	for _, event := range result.Trace.Events {
		if event.FromState != "" && event.ToState != "" {
			stateTransition = true
		}
		if event.NodeID == "" {
			continue
		}
		node, exists := nodes[event.NodeID]
		if !exists {
			t.Errorf("trace references a node from another source/model: %+v", event)
			continue
		}
		if event.Kind == "RECEIVE_COMPLETE" {
			receiveEventNodes = append(receiveEventNodes, event.NodeID)
			// Instrumentation marks the statement boundary; either the receive
			// itself or its exact containing assignment is a valid mapping.
			if (event.NodeID == receiveNodeID || event.NodeID == receiveStatementID) && event.SourceLine == node.Span.StartLine {
				receiveCompleted = true
			}
		}
	}
	if !receiveCompleted || !stateTransition {
		t.Fatalf("receive mapped=%v, runtime transitions=%v, receive event nodes=%v", receiveCompleted, stateTransition, receiveEventNodes)
	}
}

func TestExplorerSourceAndRequestSizeBoundaries(t *testing.T) {
	h := handler()
	for _, endpoint := range []string{"/api/analyze", "/api/run", "/api/build-source"} {
		t.Run(endpoint, func(t *testing.T) {
			w := post(t, h, endpoint, map[string]any{"requestId": "oversized-source", "source": strings.Repeat("a", engine.MaxSourceBytes+1)})
			if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), `"code":"source_size"`) || !strings.Contains(w.Body.String(), `"requestId":"oversized-source"`) {
				t.Fatalf("source limit: %d %s", w.Code, w.Body.String())
			}
			body := `{"requestId":"oversized-request","source":""}` + strings.Repeat(" ", maxRequestBytes)
			r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080"+endpoint, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			w = httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), `"code":"request_size"`) {
				t.Fatalf("request limit: %d %s", w.Code, w.Body.String())
			}
		})
	}
	source := "package main\nfunc main() {}\n//"
	source += strings.Repeat("x", engine.MaxSourceBytes-len(source))
	w := post(t, h, "/api/analyze", map[string]any{"requestId": "exact-source-limit", "source": source})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("exact source limit was rejected: %d %s", w.Code, w.Body.String())
	}
	body := `{"requestId":"exact-body-limit","source":"package main; func main(){}"}`
	body += strings.Repeat(" ", maxRequestBytes-len(body))
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/analyze", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("exact request limit was rejected: %d %s", w.Code, w.Body.String())
	}
}

func TestExplorerIRGeneratedSourceLimit(t *testing.T) {
	imported := engine.Import("package main; func main() { ch := make(chan int); close(ch) }")
	if imported.Status != "editable" || len(imported.IR.Symbols) != 1 {
		t.Fatalf("fixture import failed: %s", imported.Status)
	}
	// The identifier is represented once in IR, but both its declaration and
	// close operation repeat it in Go source. The JSON body remains under 4 MiB.
	imported.IR.Symbols[0].Name = strings.Repeat("c", engine.MaxSourceBytes/2+1)
	generated := engine.Generate(imported.IR)
	if generated.Status != "ok" || len(generated.Source) <= engine.MaxSourceBytes {
		t.Fatalf("fixture must produce valid oversized source: status=%s bytes=%d", generated.Status, len(generated.Source))
	}
	disabledRunner := runner.New(time.Second, time.Second)
	disabledRunner.GoPath = ""
	h := (&Server{Compiler: compiler.New(time.Second), Runner: disabledRunner}).Handler()
	for _, test := range []struct {
		endpoint string
		trace    bool
	}{
		{"/api/run", false},
		{"/api/run", true},
		{"/api/build-source", false},
	} {
		t.Run(fmt.Sprintf("%s_trace=%v", test.endpoint, test.trace), func(t *testing.T) {
			body := map[string]any{"requestId": "oversized-generated", "ir": imported.IR, "trace": test.trace}
			encoded, err := json.Marshal(body)
			if err != nil || len(encoded) >= maxRequestBytes {
				t.Fatalf("fixture request must remain below the body limit: bytes=%d err=%v", len(encoded), err)
			}
			w := post(t, h, test.endpoint, body)
			if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), `"code":"source_size"`) || !strings.Contains(w.Body.String(), `"requestId":"oversized-generated"`) {
				t.Fatalf("expanded IR bypassed source limit: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestExplorerRunRejectsRemoteRequestsAndExecutableFields(t *testing.T) {
	h := handler()
	for _, test := range []struct{ name, host, origin, code string }{
		{"remote-host", "unrelated.example", "", "host_forbidden"},
		{"remote-origin", "127.0.0.1:8080", "https://unrelated.example", "origin_forbidden"},
		{"null-origin", "127.0.0.1:8080", "null", "origin_forbidden"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://"+test.host+"/api/run", strings.NewReader(`{"requestId":"remote-run","source":"package main; func main(){}"}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", test.origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("remote run was not rejected: %d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, field := range []string{"command", "goPath", "executable"} {
		w := post(t, h, "/api/run", map[string]any{"requestId": "invalid-command", "source": demo, field: "caller-selected"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("run accepted caller-provided %s: %d %s", field, w.Code, w.Body.String())
		}
	}
}
