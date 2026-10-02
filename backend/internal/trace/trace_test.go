package trace

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"goviz/backend/internal/engine"
)

func TestInstrumentationPreservesLinesAndValidControlFlow(t *testing.T) {
	source := `package main
func main() {
 ch:=make(chan int,1)
 for i:=0;i<1;i++ { ch<-i }
 select { case x:=<-ch: _=x; default: }
 if true { close(ch) }
}`
	modified, helper, err := Instrument(source, "trace.out", 4096, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(modified, "\n") != strings.Count(source, "\n") {
		t.Fatal("line mapping changed")
	}
	for _, s := range []string{modified, helper} {
		if _, err := parser.ParseFile(token.NewFileSet(), "main.go", s, parser.AllErrors); err != nil {
			t.Fatalf("%s\n%s", err, s)
		}
	}
}

func TestSourceMappingDoesNotChooseAnOperand(t *testing.T) {
	spans := map[string]engine.Span{
		"send":  {StartByte: 20, EndByte: 40, StartLine: 3},
		"field": {StartByte: 20, EndByte: 25, StartLine: 3},
		"value": {StartByte: 30, EndByte: 35, StartLine: 3},
	}
	debug := `M=1 P=0 G=1 Log Time=100 Task=0 Category="goviz" Message="SEND_ATTEMPT:3:20"
M=1 P=0 G=1 StateTransition Time=110 Resource=Goroutine(1) Reason="chan send" GoID=1 Running->Waiting
Stack=
 main.main @ 0xff
  C:/tmp/main.go:3
M=2 P=1 G=9 StateTransition Time=120 Resource=Goroutine(9) Reason="system wait" GoID=9 Running->Waiting
`
	result := Normalize(debug, spans, 100)
	if len(result.Events) != 3 || len(result.Goroutines) != 2 {
		t.Fatalf("system goroutines must be retained: %+v", result)
	}
	if result.Events[0].NodeID != "send" || result.Events[1].NodeID != "" {
		t.Fatalf("incorrect operand mapping: %+v", result.Events)
	}
}

func TestTraceInfrastructureDoesNotMasqueradeAsUserBlocking(t *testing.T) {
	debug := `M=1 P=0 G=1 StateTransition Time=100 Resource=Goroutine(1) Reason="sleep" GoID=1 Running->Waiting
Stack=
 main.__govizStop @ 0xff
  C:/tmp/trace_support.go:5
 main.main @ 0xfe
  C:/tmp/main.go:9
M=1 P=0 G=1 Log Time=120 Task=0 Category="goviz" Message="SEND_ATTEMPT:4:30"
Stack=
 main.__govizLog @ 0xff
  C:/tmp/trace_support.go:6
 main.main @ 0xfe
  C:/tmp/main.go:4
`
	result := Normalize(debug, nil, 100)
	if len(result.Events) != 2 || !result.Events[0].Instrumentation || result.Events[0].SourceLine != 0 || result.Events[1].Instrumentation || result.Events[1].SourceLine != 4 {
		t.Fatalf("incorrect infrastructure mapping: %+v", result.Events)
	}
}

func TestNormalizeRealTransitionSemantics(t *testing.T) {
	debug := `M=1 P=0 G=1 StateTransition Time=9999999999999000 Resource=Goroutine(8) Reason="" GoID=8 NotExist->Runnable
TransitionStack=
 main.worker @ 0xff
  C:/tmp/main.go:4
Stack=
 main.main @ 0xfe
  C:/tmp/main.go:9
M=1 P=0 G=8 Log Time=9999999999999010 Task=0 Category="goviz" Message="RECEIVE_ATTEMPT:5"
M=1 P=0 G=8 StateTransition Time=9999999999999020 Resource=Goroutine(8) Reason="chan receive" GoID=8 Running->Waiting
Stack=
 main.worker @ 0xff
  C:/tmp/main.go:5
M=2 P=1 G=1 StateTransition Time=9999999999999030 Resource=Goroutine(8) Reason="" GoID=8 Waiting->Runnable
Stack=
 main.main @ 0xfe
  C:/tmp/main.go:10
`
	result := Normalize(debug, nil, 100)
	if len(result.Events) != 4 {
		t.Fatalf("%+v", result)
	}
	create, blocked, unblocked := result.Events[0], result.Events[2], result.Events[3]
	if create.GoroutineID != "8" || create.ActorGoroutineID != "1" || create.SourceLine != 4 || create.Kind != "GOROUTINE_CREATE" {
		t.Fatalf("%+v", create)
	}
	if blocked.ToState != "BLOCKED_RECEIVE" || unblocked.Kind != "UNBLOCKED" || unblocked.SourceLine != 0 || unblocked.ActorGoroutineID != "1" || unblocked.TimeNS != 30 {
		t.Fatalf("%+v %+v", blocked, unblocked)
	}
}
