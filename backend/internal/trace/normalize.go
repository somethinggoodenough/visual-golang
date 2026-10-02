package trace

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"goviz/backend/internal/engine"
)

var headerPattern = regexp.MustCompile(`^M=-?\d+ P=-?\d+ G=(-?\d+) (\w+) Time=(\d+)(.*)$`)
var transitionPattern = regexp.MustCompile(`Resource=Goroutine\((\d+)\) Reason=("(?:[^"\\]|\\.)*") GoID=\d+ (\w+)->(\w+)`)
var logPattern = regexp.MustCompile(`Category="goviz" Message="([A-Z_]+):(\d+)(?::(\d+))?"`)
var locationPattern = regexp.MustCompile(`(?:^|[/\\])main\.go:(\d+)$`)

// Normalize consumes the Go 1.24 go-tool-trace debug representation. That format
// is toolchain-specific; unknown records are ignored, never guessed into events.
// Times are relative to the earliest parsed event, avoiding JS integer overflow.
func Normalize(debug string, spans map[string]engine.Span, maxEvents int) *ExecutionTrace {
	result := &ExecutionTrace{SchemaVersion: "1.0", Events: []Event{}, Goroutines: []Goroutine{}}
	if maxEvents <= 0 {
		maxEvents = 20000
	}
	type record struct {
		event    Event
		function string
	}
	var records []record
	var block []string
	var start int64
	started := false
	flush := func() {
		if len(block) == 0 {
			return
		}
		h := headerPattern.FindStringSubmatch(block[0])
		if h == nil {
			return
		}
		timestamp, _ := strconv.ParseInt(h[3], 10, 64)
		if !started {
			start = timestamp
			started = true
		}
		e := Event{GoroutineID: h[1], TimeNS: timestamp - start}
		offset := -1
		switch h[2] {
		case "Log":
			m := logPattern.FindStringSubmatch(h[4])
			if m == nil {
				return
			}
			e.Kind = m[1]
			e.SourceLine, _ = strconv.Atoi(m[2])
			if m[3] != "" {
				offset, _ = strconv.Atoi(m[3])
			}
			if strings.HasSuffix(e.Kind, "_ATTEMPT") {
				e.Detail = "Statement reached, before evaluating its operands; blocking is reported separately by the Go runtime."
			}
		case "StateTransition":
			m := transitionPattern.FindStringSubmatch(h[4])
			if m == nil {
				return
			}
			e.GoroutineID = m[1]
			if h[1] != "-1" && h[1] != e.GoroutineID {
				e.ActorGoroutineID = h[1]
			}
			e.Detail, _ = strconv.Unquote(m[2])
			e.FromState = state(m[3], "")
			e.ToState = state(m[4], e.Detail)
			e.Kind = "STATE_CHANGE"
			switch {
			case m[3] == "NotExist":
				e.Kind = "GOROUTINE_CREATE"
			case m[4] == "NotExist":
				e.Kind = "GOROUTINE_EXIT"
			case m[4] == "Running":
				e.Kind = "RUNNING"
			case m[3] == "Waiting" && m[4] == "Runnable":
				e.Kind = "UNBLOCKED"
			case m[4] == "Runnable":
				e.Kind = "RUNNABLE"
			case m[4] == "Waiting":
				e.Kind = "BLOCKED"
			}
		default:
			return
		}
		function, currentFunction, stackKind := "", "", ""
		for _, line := range block[1:] {
			trim := strings.TrimSpace(line)
			if trim == "Stack=" || trim == "TransitionStack=" {
				stackKind = trim
				continue
			}
			if name, _, ok := strings.Cut(trim, " @ "); ok {
				currentFunction = name
				continue
			}
			// An unblocking goroutine's own stack is not the target's location.
			if e.ActorGoroutineID != "" && stackKind != "TransitionStack=" {
				continue
			}
			if h[2] == "StateTransition" && (strings.Contains(trim, "/trace_support.go:") || strings.Contains(trim, "\\trace_support.go:")) {
				e.Instrumentation = true
				e.SourceLine = 0
			}
			if m := locationPattern.FindStringSubmatch(trim); m != nil {
				if e.Instrumentation {
					continue
				}
				if e.SourceLine == 0 {
					e.SourceLine, _ = strconv.Atoi(m[1])
				}
				function = currentFunction
				break
			}
		}
		if offset >= 0 {
			e.NodeID = nodeAtOffset(spans, offset)
		} else {
			e.NodeID = nodeAtLine(spans, e.SourceLine)
		}
		if len(records) < maxEvents {
			records = append(records, record{e, function})
		} else {
			result.Truncated = true
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(debug))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "M=") {
			flush()
			block = block[:0]
		}
		block = append(block, line)
	}
	flush()
	if scanner.Err() != nil {
		result.Truncated = true
	}
	instances := map[string]*Goroutine{}
	for _, r := range records {
		e := r.event
		if e.GoroutineID == "-1" {
			continue
		}
		g := instances[e.GoroutineID]
		if g == nil {
			g = &Goroutine{ID: e.GoroutineID, State: "UNKNOWN"}
			instances[e.GoroutineID] = g
		}
		if e.FromState == "WAITING" && strings.HasPrefix(g.State, "BLOCKED_") {
			e.FromState = g.State
		}
		if e.ToState != "" {
			g.State = e.ToState
		}
		if e.SourceLine != 0 {
			g.SourceLine = e.SourceLine
			g.NodeID = e.NodeID
		}
		if r.function != "" && g.Function == "" {
			g.Function = r.function
		}
		e.ID = fmt.Sprintf("event-%d", len(result.Events)+1)
		result.Events = append(result.Events, e)
	}
	for _, g := range instances {
		result.Goroutines = append(result.Goroutines, *g)
	}
	sort.Slice(result.Goroutines, func(i, j int) bool {
		a, _ := strconv.Atoi(result.Goroutines[i].ID)
		b, _ := strconv.Atoi(result.Goroutines[j].ID)
		return a < b
	})
	return result
}

func state(raw, reason string) string {
	switch raw {
	case "Running":
		return "RUNNING"
	case "Runnable":
		return "RUNNABLE"
	case "NotExist":
		return "DONE"
	case "Waiting":
		switch {
		case strings.Contains(reason, "chan send"):
			return "BLOCKED_SEND"
		case strings.Contains(reason, "chan receive"):
			return "BLOCKED_RECEIVE"
		case strings.Contains(reason, "select"):
			return "BLOCKED_SELECT"
		default:
			return "WAITING"
		}
	case "Syscall":
		return "SYSCALL"
	default:
		return "UNKNOWN"
	}
}

func nodeAtLine(spans map[string]engine.Span, line int) string {
	if line <= 0 {
		return ""
	}
	best := ""
	for id, s := range spans {
		if s.StartLine == line {
			if best != "" {
				return ""
			}
			best = id
		}
	}
	return best
}

// Annotation positions are original byte offsets. Prefer the largest node
// starting exactly there: a full send/assignment over its channel/field operand.
// If spans are indistinguishable, keep only the reliable line mapping.
func nodeAtOffset(spans map[string]engine.Span, offset int) string {
	best, size := "", -1
	ambiguous := false
	for id, s := range spans {
		if s.StartByte != offset {
			continue
		}
		n := s.EndByte - s.StartByte
		if n > size {
			best = id
			size = n
			ambiguous = false
		} else if n == size {
			ambiguous = true
		}
	}
	if ambiguous {
		return ""
	}
	return best
}
