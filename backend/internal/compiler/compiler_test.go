package compiler

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"goviz/backend/internal/engine"
)

const example = `package main
import "fmt"
func main() {
 ch := make(chan int)
 go func() { ch <- 42 }()
 value := <-ch
 fmt.Println(value)
}`

func TestRealGoBuild(t *testing.T) {
	c := New(45 * time.Second)
	if c.GoPath == "" {
		t.Skip("Go is not installed")
	}
	for _, source := range []string{example, strings.Replace(example, "chan int)", "chan int, 1)", 1), `package main; import "fmt"; func main(){ ch:=make(chan int, 1); ch<-9; close(ch); _,ok:=<-ch; fmt.Println(ok) }`} {
		p := engine.Import(source)
		if p.Status != "editable" {
			t.Fatalf("import: %+v", p)
		}
		r := c.Build(context.Background(), p.IR)
		if r.Status != "ok" || r.ToolchainVersion == "" || r.SourceHash == "" {
			t.Fatalf("real build: %+v", r)
		}
	}
}

func TestUnavailableAndInvalidIR(t *testing.T) {
	c := New(time.Second)
	c.GoPath = ""
	if r := c.Build(context.Background(), nil); r.Status != "toolchain_unavailable" {
		t.Fatalf("got %+v", r)
	}
	c = New(time.Second)
	if c.GoPath != "" {
		if r := c.Build(context.Background(), nil); r.Status != "compile_error" || len(r.Diagnostics) == 0 {
			t.Fatalf("got %+v", r)
		}
	}
}

func TestCompilerTimeoutAndFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix only")
	}
	p := engine.Import(example)
	if p.IR == nil {
		t.Fatal(p.Diagnostics)
	}
	for _, tt := range []struct {
		name, script, status string
		timeout              time.Duration
	}{
		{"timeout", "exec sleep 3", "timeout", 40 * time.Millisecond},
		{"error", "echo 'main.go:6:2: deliberate test compiler failure' >&2\nexit 1", "compile_error", time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "go")
			body := "#!/bin/sh\nif [ \"$1\" = \"version\" ]; then echo 'test toolchain'; exit 0; fi\n" + tt.script + "\n"
			if err := os.WriteFile(path, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			c := New(tt.timeout)
			c.GoPath = path
			r := c.Build(context.Background(), p.IR)
			if r.Status != tt.status || len(r.Diagnostics) == 0 {
				t.Fatalf("got %+v", r)
			}
		})
	}
}

func TestLimitedOutputAndEnvironment(t *testing.T) {
	b := &limitedBuffer{limit: 8}
	if n, err := b.Write([]byte("abcdefghijkl")); n != 12 || err != nil || b.String() != "abcdefgh" {
		t.Fatal("output limit broken")
	}
	t.Setenv("GOFLAGS", "-toolexec=unwanted")
	t.Setenv("GOTOOLCHAIN", "auto")
	env := strings.Join(environment(), "\n")
	if !strings.Contains(env, "GOTOOLCHAIN=local") || strings.Contains(env, "unwanted") || !strings.Contains(env, "GOPROXY=off") {
		t.Fatal(env)
	}
}

func TestBuildDiagnosticMustContainErrorOffset(t *testing.T) {
	source := "package main\n// 中文\nvalue, ok := <-ch\n"
	start := strings.Index(source, "value")
	spans := map[string]engine.Span{
		"statement": {File: "main.go", StartByte: start, EndByte: start + len("value, ok := <-ch"), StartLine: 3, StartColumn: 1},
		"ok_symbol": {File: "main.go", StartByte: start + 7, EndByte: start + 9, StartLine: 3, StartColumn: 8},
	}
	ds := buildDiagnostics("main.go:3:14: channel error", source, spans)
	if len(ds) != 1 || ds[0].NodeID != "statement" {
		t.Fatalf("incorrect source mapping: %+v", ds)
	}
}
