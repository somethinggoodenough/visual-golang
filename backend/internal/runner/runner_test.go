package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"goviz/backend/internal/engine"
)

func testRunner(t *testing.T) *Runner {
	t.Helper()
	r := New(45*time.Second, 2*time.Second)
	if r.GoPath == "" {
		t.Skip("Go toolchain unavailable")
	}
	return r
}

func TestRealExecutionStatuses(t *testing.T) {
	r := testRunner(t)
	for _, tt := range []struct{ name, source, status, output string }{
		{"success", `package main; import "fmt"; func main(){fmt.Println("hello")}`, "ok", "hello\n"},
		{"compile", `package main; func main(){missing()}`, "compile_error", ""},
		{"panic", `package main; func main(){panic("intentional")}`, "runtime_error", ""},
		{"deadlock", `package main; func main(){<-make(chan int)}`, "deadlock", ""},
		{"dependency", `package main; import _ "example.com/dependency"; func main(){}`, "compile_error", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Run(context.Background(), tt.source, Options{})
			if result.Status != tt.status || result.Stdout != tt.output {
				t.Fatalf("%+v", result)
			}
			if result.SourceHash == "" {
				t.Fatal("source hash missing")
			}
		})
	}
}

func TestCompileOnlyDoesNotExecute(t *testing.T) {
	r := testRunner(t)
	result := r.Run(context.Background(), `package main; func main(){panic("must not run")}`, Options{CompileOnly: true})
	if result.Status != "ok" || result.ExitCode != nil {
		t.Fatalf("%+v", result)
	}
}

func TestTimeoutAndOutputLimit(t *testing.T) {
	r := testRunner(t)
	r.OutputLimit = 128
	result := r.Run(context.Background(), `package main; import "fmt"; func main(){for i:=0;i<1000;i++ {fmt.Println("output")}}`, Options{})
	if result.Status != "ok" || !result.OutputTruncated || len(result.Stdout)+len(result.Stderr) > 128 {
		t.Fatalf("%+v", result)
	}
	r.RunTimeout = 150 * time.Millisecond
	result = r.Run(context.Background(), `package main; func main(){for {}}`, Options{})
	if result.Status != "timeout" {
		t.Fatalf("%+v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = r.Run(ctx, `package main; func main(){}`, Options{})
	if result.Status != "cancelled" {
		t.Fatalf("%+v", result)
	}
}

func TestRunningCancellation(t *testing.T) {
	r := testRunner(t)
	r.RunTimeout = 10 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(2*time.Second, cancel)
	defer timer.Stop()
	started := time.Now()
	result := r.Run(ctx, `package main; func main(){for {}}`, Options{})
	if result.Status != "cancelled" || time.Since(started) > 4*time.Second {
		t.Fatalf("%+v", result)
	}
}

func TestTraceControlFlowAndBoundedFile(t *testing.T) {
	r := testRunner(t)
	source := `package main
import "fmt"
var time,trace,os,context,sync,strconv = 1,2,3,4,5,6
func main() {
 ch:=make(chan int,1)
 for i:=0;i<1;i++ { ch<-i }
 select { case x:=<-ch: fmt.Println(x); default: }
 close:=func(ch chan int){fmt.Println("shadowed")}
 close(ch)
}`
	result := r.Run(context.Background(), source, Options{Trace: true})
	if result.Status != "ok" || result.Stdout != "0\nshadowed\n" || result.Trace == nil {
		t.Fatalf("status=%s out=%q stderr=%s diagnostics=%+v", result.Status, result.Stdout, result.Stderr, result.Diagnostics)
	}
	chosen, received := false, false
	for _, event := range result.Trace.Events {
		if event.Kind == "SELECT_CASE_CHOSEN" {
			chosen = true
		}
		if event.Kind == "RECEIVE_COMPLETE" {
			received = true
		}
		if event.Kind == "CHANNEL_CLOSE" || event.Kind == "CHANNEL_CLOSE_ATTEMPT" {
			t.Fatalf("shadowed close was labeled builtin: %+v", event)
		}
	}
	if !chosen || !received {
		t.Fatalf("selected receive completion not recorded: %+v", result.Trace.Events)
	}
	result = r.Run(context.Background(), `package main; func main(){panic("intentional traced panic")}`, Options{Trace: true})
	if result.Status != "runtime_error" || result.Trace == nil || result.TraceWarning != "" {
		t.Fatalf("traced panic: status=%s warning=%s", result.Status, result.TraceWarning)
	}
	r.TraceLimit = 128
	result = r.Run(context.Background(), `package main; func main(){}`, Options{Trace: true})
	if result.Status != "ok" || result.TraceWarning == "" || len(result.NativeTrace) > 128 {
		t.Fatalf("status=%s warning=%s trace length=%d", result.Status, result.TraceWarning, len(result.NativeTrace))
	}
}

func TestRealTraceAndSourceMapping(t *testing.T) {
	r := testRunner(t)
	source := `package main
import "fmt"
func main() {
 ch := make(chan int)
 go func() { ch <- 42 }()
 value := <-ch
 fmt.Println(value)
}`
	start := strings.Index(source, "value := <-ch")
	result := r.Run(context.Background(), source, Options{Trace: true, SourceMap: map[string]engine.Span{"receive": {StartLine: 6, StartByte: start, EndByte: start + len("value := <-ch")}}})
	if result.Status != "ok" || result.Stdout != "42\n" || result.Trace == nil || len(result.NativeTrace) == 0 || result.TraceWarning != "" {
		t.Fatalf("status=%s stderr=%s trace=%+v warning=%s diagnostics=%+v", result.Status, result.Stderr, result.Trace, result.TraceWarning, result.Diagnostics)
	}
	if len(result.Trace.Goroutines) < 2 {
		t.Fatalf("missing real goroutines: %+v", result.Trace)
	}
	found, transition := false, false
	for _, e := range result.Trace.Events {
		if e.Kind == "RECEIVE_COMPLETE" && e.SourceLine == 6 && e.NodeID == "receive" {
			found = true
		}
		if e.FromState != "" && e.ToState != "" {
			transition = true
		}
	}
	if !found || !transition {
		t.Fatalf("missing source-mapped trace events: %+v", result.Trace.Events)
	}
}

func TestTracedTimeoutPreservesBlockedEvidence(t *testing.T) {
	r := testRunner(t)
	r.RunTimeout = 150 * time.Millisecond
	result := r.Run(context.Background(), `package main
func main(){ <-make(chan int) }`, Options{Trace: true})
	if result.Status != "timeout" || result.Trace == nil || result.TraceWarning != "" {
		t.Fatalf("status=%s warning=%s trace=%+v stderr=%s", result.Status, result.TraceWarning, result.Trace, result.Stderr)
	}
	found := false
	for _, e := range result.Trace.Events {
		if e.ToState == "BLOCKED_RECEIVE" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no runtime blocking evidence: %+v", result.Trace.Events)
	}
}

func TestCombinedOutputAndEnvironment(t *testing.T) {
	b := &outputBuffer{limit: 8}
	_, _ = outputWriter{b, false}.Write([]byte("12345"))
	_, _ = outputWriter{b, true}.Write([]byte("67890"))
	stdout, stderr, truncated := b.snapshot()
	if stdout != "12345" || stderr != "678" || !truncated {
		t.Fatalf("%q %q %v", stdout, stderr, truncated)
	}
	t.Setenv("GOFLAGS", "-toolexec=unwanted")
	t.Setenv("GOROOT", "unwanted")
	t.Setenv("GOWORK", "unwanted")
	env := strings.Join(environment(t.TempDir()), "\n")
	if strings.Contains(env, "unwanted") || !strings.Contains(env, "GOTOOLCHAIN=local") || !strings.Contains(env, "GOPROXY=off") {
		t.Fatal(env)
	}
}
