// Package compiler validates code with the installed Go toolchain. It never
// executes the resulting program and never accepts commands or paths from HTTP.
package compiler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"goviz/backend/internal/engine"
)

type Result struct {
	Status           string              `json:"status"`
	ToolchainVersion string              `json:"toolchainVersion"`
	DurationMS       int64               `json:"durationMs"`
	SourceHash       string              `json:"sourceHash,omitempty"`
	Diagnostics      []engine.Diagnostic `json:"diagnostics"`
}

type Compiler struct {
	GoPath  string
	Timeout time.Duration
	slots   chan struct{}
}

func New(timeout time.Duration) *Compiler {
	path, _ := exec.LookPath("go")
	return &Compiler{GoPath: path, Timeout: timeout, slots: make(chan struct{}, 2)}
}

func (c *Compiler) Version(ctx context.Context) (string, bool) {
	if c.GoPath == "" {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.GoPath, "version")
	cmd.Env = environment()
	data, err := cmd.Output()
	return strings.TrimSpace(string(data)), err == nil
}

func (c *Compiler) Build(ctx context.Context, p *engine.Program) (result Result) {
	started := time.Now()
	result.Diagnostics = []engine.Diagnostic{}
	defer func() { result.DurationMS = time.Since(started).Milliseconds() }()
	version, available := c.Version(ctx)
	result.ToolchainVersion = version
	if !available {
		result.Status = "toolchain_unavailable"
		result.Diagnostics = []engine.Diagnostic{diagnostic("toolchain_unavailable", "未找到可用的 Go 工具链。请从 https://go.dev/dl/ 安装 Go，并重启本地服务。")}
		return
	}
	generated := engine.Generate(p)
	if generated.Status != "ok" {
		result.Status, result.Diagnostics = "compile_error", generated.Diagnostics
		return
	}
	result.SourceHash = generated.SourceHash
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		result.Status = "timeout"
		result.Diagnostics = []engine.Diagnostic{diagnostic("build_timeout", "等待编译超时。")}
		return
	}
	dir, err := os.MkdirTemp("", "goviz-build-")
	if err != nil {
		return failure(result, err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(generated.Source), 0600); err != nil {
		return failure(result, err)
	}
	output := &limitedBuffer{limit: 64 << 10}
	cmd := exec.CommandContext(ctx, c.GoPath, "build", "-o", filepath.Join(dir, "program"), "main.go")
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, environment(), output, output
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if ctx.Err() != nil {
		result.Status = "timeout"
		result.Diagnostics = []engine.Diagnostic{diagnostic("build_timeout", fmt.Sprintf("Go 编译超过 %s 或请求已取消。", c.Timeout))}
	} else if err != nil {
		result.Status = "compile_error"
		message := strings.TrimSpace(output.String())
		if message == "" {
			message = err.Error()
		}
		result.Diagnostics = buildDiagnostics(message, generated.Source, generated.SourceMap)
	} else {
		result.Status = "ok"
	}
	return
}

func failure(r Result, err error) Result {
	r.Status = "compile_error"
	r.Diagnostics = []engine.Diagnostic{diagnostic("build_io", err.Error())}
	return r
}
func diagnostic(code, message string) engine.Diagnostic {
	return engine.Diagnostic{Code: code, Severity: "error", Phase: "build", Message: message}
}

// Disable user go env/flags, network dependency lookup, auto toolchain downloads,
// cross compilation, cgo, and ambient workspace configuration.
func environment() []string {
	replace := map[string]string{"GOENV": "off", "GOFLAGS": "", "GOWORK": "off", "GO111MODULE": "off", "GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local", "CGO_ENABLED": "0", "GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH}
	result := []string{}
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if _, exists := replace[key]; !exists {
			result = append(result, e)
		}
	}
	for k, v := range replace {
		result = append(result, k+"="+v)
	}
	return result
}

type limitedBuffer struct {
	mu    sync.Mutex
	b     bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remaining := b.limit - b.b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.b.Write(p)
	}
	return n, nil
}
func (b *limitedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

func buildDiagnostics(message, source string, spans map[string]engine.Span) []engine.Diagnostic {
	ds := []engine.Diagnostic{}
	for _, line := range strings.Split(message, "\n") {
		if strings.HasPrefix(line, "# ") {
			continue
		}
		d := diagnostic("go_build", line)
		parts := strings.SplitN(line, ":", 4)
		if len(parts) == 4 && strings.HasSuffix(parts[0], "main.go") {
			ln, e1 := strconv.Atoi(parts[1])
			col, e2 := strconv.Atoi(parts[2])
			smallest := int(^uint(0) >> 1)
			if errors.Join(e1, e2) == nil {
				offset := 0
				lines := strings.SplitAfter(source, "\n")
				if ln < 1 || ln > len(lines) || col < 1 || col > len(lines[ln-1])+1 {
					ds = append(ds, d)
					continue
				}
				for _, sourceLine := range lines[:ln-1] {
					offset += len(sourceLine)
				}
				offset += col - 1
				for id, s := range spans {
					if s.StartByte <= offset && offset < s.EndByte && s.EndByte-s.StartByte < smallest {
						v := s
						d.NodeID = id
						d.Span = &v
						smallest = s.EndByte - s.StartByte
					}
				}
			}
		}
		ds = append(ds, d)
	}
	return ds
}
