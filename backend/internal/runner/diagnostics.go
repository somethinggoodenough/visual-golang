package runner

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"goviz/backend/internal/engine"
)

var sourceDiagnosticPattern = regexp.MustCompile(`(?:^|[/\\])main\.go:(\d+):(\d+):\s*(.*)$`)

// Instrumented builds preserve source lines but insert columns. Only plain
// builds may use the reported byte column to select a precise source node.
func sourceDiagnostics(code, message, source string, spans map[string]engine.Span, instrumented bool) []engine.Diagnostic {
	result := []engine.Diagnostic{}
	lines := strings.SplitAfter(source, "\n")
	for _, line := range strings.Split(message, "\n") {
		if strings.HasPrefix(line, "# ") || strings.TrimSpace(line) == "" {
			continue
		}
		d := engine.Diagnostic{Code: code, Message: line, Severity: "error", Phase: "build"}
		m := sourceDiagnosticPattern.FindStringSubmatch(line)
		if m != nil {
			ln, _ := strconv.Atoi(m[1])
			col, _ := strconv.Atoi(m[2])
			if ln >= 1 && ln <= len(lines) && col >= 1 {
				start := 0
				for _, prior := range lines[:ln-1] {
					start += len(prior)
				}
				text := strings.TrimRight(lines[ln-1], "\r\n")
				end := start + len(text)
				if instrumented {
					col = 1
				} else if col <= len(text)+1 {
					start += col - 1
					_, width := utf8.DecodeRuneInString(source[start:end])
					end = start + width
				} else {
					result = append(result, d)
					continue
				}
				d.Span = &engine.Span{File: "main.go", StartByte: start, EndByte: end, StartLine: ln, StartColumn: col}
				if !instrumented {
					best := int(^uint(0) >> 1)
					for id, s := range spans {
						if s.StartByte <= start && start < s.EndByte && (s.EndByte-s.StartByte < best || s.EndByte-s.StartByte == best && id < d.NodeID) {
							d.NodeID = id
							best = s.EndByte - s.StartByte
						}
					}
				}
			}
		}
		result = append(result, d)
	}
	if len(result) == 0 {
		return diagnostic(code, message, "build")
	}
	return result
}
