package api

import (
	"net/http"

	"goviz/backend/internal/analysis"
	"goviz/backend/internal/engine"
	"goviz/backend/internal/runner"
)

func analyzeSource(w http.ResponseWriter, r *http.Request) {
	var req request
	if !read(w, r, &req) || !checkSourceSize(w, req.RequestID, req.Source) {
		return
	}
	result := analysis.Analyze(req.Source)
	write(w, 200, struct {
		RequestID       string `json:"requestId"`
		ProgramRevision int64  `json:"programRevision"`
		analysis.Result
	}{req.RequestID, req.ProgramRevision, result})
}

func checkSourceSize(w http.ResponseWriter, requestID, source string) bool {
	if len(source) > engine.MaxSourceBytes {
		apiError(w, 413, requestID, "source_size", "源码不能超过 1 MiB。")
		return false
	}
	return true
}

func executeSource(localRunner *runner.Runner, compileOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req request
		if !read(w, r, &req) || !checkSourceSize(w, req.RequestID, req.Source) {
			return
		}
		source := req.Source
		if req.IR != nil {
			if source != "" {
				apiError(w, 400, req.RequestID, "source_conflict", "Provide either source or IR, not both.")
				return
			}
			if !checkIRVersion(w, req) {
				return
			}
			generated := engine.Generate(req.IR)
			if generated.Status != "ok" {
				write(w, 200, conversionResponse{req.RequestID, req.ProgramRevision, generated})
				return
			}
			source = generated.Source
			// Compact IR can expand into larger source when names are reused.
			// Enforce the same source limit before analysis or any build/run.
			if !checkSourceSize(w, req.RequestID, source) {
				return
			}
		}
		spans := map[string]engine.Span{}
		if req.Trace && !compileOnly {
			result := analysis.Analyze(source)
			if result.Model != nil {
				for _, node := range result.Model.Nodes {
					spans[node.ID] = node.Span
				}
			}
		}
		result := localRunner.Run(r.Context(), source, runner.Options{Trace: req.Trace, CompileOnly: compileOnly, SourceMap: spans})
		write(w, 200, struct {
			RequestID       string `json:"requestId"`
			ProgramRevision int64  `json:"programRevision"`
			runner.Result
		}{req.RequestID, req.ProgramRevision, result})
	}
}
