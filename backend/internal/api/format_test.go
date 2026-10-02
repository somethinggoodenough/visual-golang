package api

import (
	"encoding/json"
	"strings"
	"testing"

	"goviz/backend/internal/engine"
)

func TestFormatSourceDraft(t *testing.T) {
	source := "package main\n// 中文 😀\nfunc main(){ch:=make(chan int,1);ch<-3; <-ch}"
	w := post(t, handler(), "/api/format", map[string]any{"requestId": "format-1", "programRevision": 4, "source": source})
	var result conversionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || result.Status != "ok" || result.RequestID != "format-1" || result.ProgramRevision != 4 || !strings.Contains(result.Source, "ch := make(chan int, 1)") || result.Source == source {
		t.Fatal(w.Body.String())
	}
	before, after := engine.Import(source), engine.Import(result.Source)
	if before.Status != "editable" || after.Status != "editable" || engine.Normalize(before.IR) != engine.Normalize(after.IR) {
		t.Fatal("format changed semantics")
	}
	if result.SourceHash == "" || !strings.Contains(result.Source, "中文 😀") {
		t.Fatal("lost source metadata")
	}
	w = post(t, handler(), "/api/format", map[string]any{"requestId": "again", "source": result.Source})
	var again conversionResponse
	_ = json.Unmarshal(w.Body.Bytes(), &again)
	if again.Source != result.Source {
		t.Fatal("format is not idempotent")
	}
}

func TestFormatInvalidAndExtendedSource(t *testing.T) {
	for _, source := range []string{"package main\nfunc main( {", "package main\nfunc main(){ for i:=0;i<2;i++ { println(i) } }"} {
		w := post(t, handler(), "/api/format", map[string]any{"requestId": "x", "source": source})
		var result conversionResponse
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		if strings.Contains(source, "main( {") {
			if result.Status != "invalid" || result.Source != source || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span == nil {
				t.Fatal(w.Body.String())
			}
		} else if result.Status != "ok" {
			t.Fatal(w.Body.String())
		}
	}
	w := post(t, handler(), "/api/format", map[string]any{"requestId": "big", "source": strings.Repeat("a", engine.MaxSourceBytes+1)})
	if w.Code != 413 {
		t.Fatal(w.Code)
	}
}
