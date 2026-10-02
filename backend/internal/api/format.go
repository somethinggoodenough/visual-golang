package api

import (
	"crypto/sha256"
	"fmt"
	"go/format"
	"go/scanner"
	"net/http"
	"unicode/utf8"

	"goviz/backend/internal/engine"
)

// Formatting is a source-draft operation: it neither applies a graph revision
// nor requires the program to belong to the editable subset.
func formatSource(w http.ResponseWriter, r *http.Request) {
	var req request
	if !read(w, r, &req) {
		return
	}
	if len(req.Source) > engine.MaxSourceBytes {
		apiError(w, 413, req.RequestID, "source_size", "源码不能超过 1 MiB。")
		return
	}
	result := engine.Result{Status: "ok", Source: req.Source, Diagnostics: []engine.Diagnostic{}}
	if !utf8.ValidString(req.Source) {
		result.Status = "invalid"
		result.Diagnostics = append(result.Diagnostics, engine.Diagnostic{Code: "invalid_encoding", Severity: "error", Phase: "parse", Message: "Source must contain valid UTF-8."})
	} else if formatted, err := format.Source([]byte(req.Source)); err != nil {
		result.Status = "invalid"
		if errors, ok := err.(scanner.ErrorList); ok {
			for _, item := range errors {
				end := item.Pos.Offset
				if end < len(req.Source) {
					_, width := utf8.DecodeRuneInString(req.Source[end:])
					end += width
				}
				result.Diagnostics = append(result.Diagnostics, engine.Diagnostic{Code: "format_syntax", Severity: "error", Phase: "parse", Message: item.Msg, Span: &engine.Span{File: "main.go", StartByte: item.Pos.Offset, EndByte: end, StartLine: item.Pos.Line, StartColumn: item.Pos.Column}})
			}
		} else {
			result.Diagnostics = append(result.Diagnostics, engine.Diagnostic{Code: "format_syntax", Severity: "error", Phase: "parse", Message: err.Error()})
		}
	} else {
		result.Source = string(formatted)
	}
	result.SourceHash = fmt.Sprintf("%x", sha256.Sum256([]byte(result.Source)))
	write(w, 200, conversionResponse{req.RequestID, req.ProgramRevision, result})
}
