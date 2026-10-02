// Package runner executes trusted local single-file Go programs. Time and output
// bounds protect the UI from accidents; this is not an untrusted-code sandbox.
package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"goviz/backend/internal/engine"
	tracing "goviz/backend/internal/trace"
)

type Options struct {
	Trace       bool
	CompileOnly bool
	SourceMap   map[string]engine.Span
}

type Result struct {
	Status          string                  `json:"status"`
	Stdout          string                  `json:"stdout"`
	Stderr          string                  `json:"stderr"`
	ExitCode        *int                    `json:"exitCode"`
	DurationMS      int64                   `json:"durationMs"`
	SourceHash      string                  `json:"sourceHash"`
	OutputTruncated bool                    `json:"outputTruncated"`
	Diagnostics     []engine.Diagnostic     `json:"diagnostics"`
	Trace           *tracing.ExecutionTrace `json:"trace,omitempty"`
	NativeTrace     []byte                  `json:"nativeTrace,omitempty"`
	TraceWarning    string                  `json:"traceWarning,omitempty"`
}

type Runner struct {
	GoPath       string
	BuildTimeout time.Duration
	RunTimeout   time.Duration
	OutputLimit  int
	TraceLimit   int
	slots        chan struct{}
}

func New(buildTimeout, runTimeout time.Duration) *Runner {
	path, _ := exec.LookPath("go")
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if buildTimeout <= 0 {
		buildTimeout = 30 * time.Second
	}
	if runTimeout <= 0 {
		runTimeout = 5 * time.Second
	}
	return &Runner{GoPath: path, BuildTimeout: buildTimeout, RunTimeout: runTimeout, OutputLimit: 64 << 10, TraceLimit: 4 << 20, slots: make(chan struct{}, 2)}
}

func (r *Runner) Run(ctx context.Context, source string, options Options) (result Result) {
	started := time.Now()
	result.Diagnostics = []engine.Diagnostic{}
	hash := sha256.Sum256([]byte(source))
	result.SourceHash = hex.EncodeToString(hash[:])
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	if r.GoPath == "" {
		result.Status = "toolchain_unavailable"
		result.Diagnostics = diagnostic("toolchain_unavailable", "Go toolchain is not available.", "build")
		return
	}
	buildCtx, cancelBuild := context.WithTimeout(ctx, r.BuildTimeout)
	defer cancelBuild()
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-buildCtx.Done():
		result.Status = contextStatus(ctx)
		return
	}
	// Reject non-standard imports before invoking go build. GO111MODULE=off
	// alone still permits ambient GOPATH packages, which this runner does not.
	if err := r.validateImports(buildCtx, source); err != nil {
		result.Status = "compile_error"
		if buildCtx.Err() != nil {
			result.Status = contextStatus(ctx)
		}
		result.Diagnostics = sourceDiagnostics("source_import", err.Error(), source, options.SourceMap, false)
		return
	}
	dir, err := os.MkdirTemp("", "goviz-run-")
	if err != nil {
		result.Status = "runtime_error"
		result.Diagnostics = diagnostic("run_io", err.Error(), "run")
		return
	}
	defer os.RemoveAll(dir)
	tracePath := filepath.Join(dir, "execution.trace")
	buildSource := source
	args := []string{"build", "-o", filepath.Join(dir, "program.exe"), "main.go"}
	if options.Trace && !options.CompileOnly {
		var helper string
		buildSource, helper, err = tracing.Instrument(source, tracePath, r.TraceLimit, r.RunTimeout)
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, "trace_support.go"), []byte(helper), 0600)
			args = append(args, "trace_support.go")
		}
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(buildSource), 0600)
	}
	if err != nil {
		result.Status = "compile_error"
		result.Diagnostics = sourceDiagnostics("source", err.Error(), source, options.SourceMap, false)
		return
	}
	buildOutput := &outputBuffer{limit: r.OutputLimit}
	cmd := exec.CommandContext(buildCtx, r.GoPath, args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, environment(dir), outputWriter{buildOutput, false}, outputWriter{buildOutput, true}
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if buildCtx.Err() != nil {
		result.Status = contextStatus(ctx)
		result.Diagnostics = diagnostic("build_timeout", "Build timed out or was cancelled.", "build")
		return
	}
	if err != nil {
		result.Status = "compile_error"
		result.Stdout, result.Stderr, result.OutputTruncated = buildOutput.snapshot()
		message := strings.TrimSpace(result.Stderr + result.Stdout)
		if message == "" {
			message = err.Error()
		}
		result.Diagnostics = sourceDiagnostics("go_build", message, source, options.SourceMap, options.Trace && !options.CompileOnly)
		return
	}
	if options.CompileOnly {
		result.Status = "ok"
		return
	}
	cancelBuild()
	timeout := r.RunTimeout
	if options.Trace {
		timeout += 300 * time.Millisecond
	}
	runCtx, cancelRun := context.WithTimeout(ctx, timeout)
	defer cancelRun()
	output := &outputBuffer{limit: r.OutputLimit}
	cmd = exec.CommandContext(runCtx, filepath.Join(dir, "program.exe"))
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, environment(dir), outputWriter{output, false}, outputWriter{output, true}
	cmd.WaitDelay = 100 * time.Millisecond
	err = cmd.Run()
	result.Stdout, result.Stderr, result.OutputTruncated = output.snapshot()
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		result.ExitCode = &code
	}
	switch {
	case ctx.Err() != nil:
		result.Status = "cancelled"
	case runCtx.Err() != nil || fileExists(tracePath+".timeout"):
		result.Status = "timeout"
	case err == nil:
		result.Status = "ok"
	case strings.Contains(result.Stderr, "fatal error: all goroutines are asleep - deadlock!"):
		result.Status = "deadlock"
	default:
		result.Status = "runtime_error"
	}
	if options.Trace && ctx.Err() == nil {
		r.collectTrace(ctx, dir, tracePath, options.SourceMap, &result)
	}
	return
}

func (r *Runner) collectTrace(ctx context.Context, dir, path string, spans map[string]engine.Span, result *Result) {
	f, err := os.Open(path)
	if err != nil {
		result.TraceWarning = "No runtime trace was produced."
		return
	}
	data, readErr := io.ReadAll(io.LimitReader(f, int64(r.TraceLimit)+1))
	f.Close()
	if readErr != nil || len(data) == 0 {
		result.TraceWarning = "The runtime trace could not be read."
		return
	}
	if len(data) > r.TraceLimit || fileExists(path+".truncated") {
		result.TraceWarning = "Runtime trace exceeded its size limit; no incomplete native trace is offered."
		return
	}
	result.NativeTrace = data
	parseCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output := &outputBuffer{limit: 16 << 20}
	cmd := exec.CommandContext(parseCtx, r.GoPath, "tool", "trace", "-d=parsed", path)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, environment(dir), outputWriter{output, false}, outputWriter{output, true}
	cmd.WaitDelay = 100 * time.Millisecond
	err = cmd.Run()
	stdout, stderr, truncated := output.snapshot()
	// Older Go releases used the numeric spelling for parsed debug events.
	if err != nil && strings.Contains(stderr, "invalid value") && strings.Contains(stderr, "-d") {
		output = &outputBuffer{limit: 16 << 20}
		cmd = exec.CommandContext(parseCtx, r.GoPath, "tool", "trace", "-d=1", path)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, environment(dir), outputWriter{output, false}, outputWriter{output, true}
		cmd.WaitDelay = 100 * time.Millisecond
		err = cmd.Run()
		stdout, stderr, truncated = output.snapshot()
	}
	result.Trace = tracing.Normalize(stdout, spans, 20000)
	result.Trace.Truncated = result.Trace.Truncated || truncated || err != nil
	if err != nil {
		result.TraceWarning = "Trace decoding was incomplete: " + strings.TrimSpace(stderr)
	}
	if truncated {
		result.TraceWarning = "Trace replay was limited to bounded decoder output; the native trace is still available."
	}
	if len(result.Trace.Events) == 0 && result.TraceWarning == "" {
		result.TraceWarning = "No source-mapped events could be decoded with this Go toolchain."
	}
}

func (r *Runner) validateImports(ctx context.Context, source string) error {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", source, parser.ImportsOnly)
	if err != nil {
		return err
	}
	if f.Name.Name != "main" {
		return errors.New("execution requires package main")
	}
	// The executable is a server-selected Go installation, never an HTTP path.
	if len(f.Imports) == 0 {
		return nil
	}
	rootCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(rootCtx, r.GoPath, "env", "GOROOT")
	cmd.Env = environment(filepath.Dir(r.GoPath))
	cmd.WaitDelay = 100 * time.Millisecond
	output := &outputBuffer{limit: 4096}
	cmd.Stdout, cmd.Stderr = outputWriter{output, false}, outputWriter{output, true}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot identify Go standard library: %w", err)
	}
	stdout, _, truncated := output.snapshot()
	goRoot := strings.TrimSpace(stdout)
	if truncated || !filepath.IsAbs(goRoot) {
		return errors.New("Go toolchain returned an invalid GOROOT")
	}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return err
		}
		if path == "C" || strings.Contains(path, ".") || strings.Contains(path, "\\") || strings.HasPrefix(path, "/") || strings.Contains(path, ":") || strings.Contains(path, "vendor/") || strings.Contains(path, "internal/") {
			return fmt.Errorf("only standard-library imports are supported: %s", path)
		}
		info, err := os.Stat(filepath.Join(goRoot, "src", filepath.FromSlash(path)))
		if err != nil || !info.IsDir() {
			return fmt.Errorf("only standard-library imports are supported: %s", path)
		}
	}
	return nil
}

func contextStatus(ctx context.Context) string {
	if ctx.Err() != nil {
		return "cancelled"
	}
	return "timeout"
}
func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
func diagnostic(code, message, phase string) []engine.Diagnostic {
	return []engine.Diagnostic{{Code: code, Message: message, Severity: "error", Phase: phase}}
}

// Keep essential host executable/temp/cache environment, while removing all Go
// overrides (including lower-case variants on Windows) and disabling downloads.
func environment(dir string) []string {
	result := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if upper != "GOCACHE" && (strings.HasPrefix(upper, "GO") || strings.HasPrefix(upper, "CGO")) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "GOENV=off", "GOFLAGS=", "GOWORK=off", "GO111MODULE=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH, "GOPATH="+filepath.Join(dir, "gopath"))
}

type outputBuffer struct {
	mu             sync.Mutex
	stdout, stderr bytes.Buffer
	limit, n       int
	truncated      bool
}
type outputWriter struct {
	buffer *outputBuffer
	stderr bool
}

func (w outputWriter) Write(p []byte) (int, error) {
	b := w.buffer
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := b.limit - b.n
	if left < 0 {
		left = 0
	}
	if len(p) > left {
		p = p[:left]
		b.truncated = true
	}
	if w.stderr {
		_, _ = b.stderr.Write(p)
	} else {
		_, _ = b.stdout.Write(p)
	}
	b.n += len(p)
	return n, nil
}
func (b *outputBuffer) snapshot() (string, string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stdout.String(), b.stderr.String(), b.truncated
}
