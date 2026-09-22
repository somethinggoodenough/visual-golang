package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestStrictUnionJSON(t *testing.T) {
	for _, data := range []string{
		`{"id":"x","kind":"make_channel","symbolId":"ch"}`,
		`{"id":"x","kind":"make_channel","symbolId":"ch","capacity":null}`,
		`{"id":"x","kind":"make_channel","symbolId":"ch","capacity":1.2}`,
		`{"id":"x","kind":"receive","channelSymbolId":"ch","bindings":null}`,
		`{"id":"x","kind":"return","value":{"id":"e","kind":"int_literal","value":"1"}}`,
		`{"id":"x","kind":"unknown"}`,
		`{"id":"x","kind":"print","value":{"id":"e","kind":"int_literal","value":42}}`,
		`{"id":"x","kind":"print","value":{"id":"e","kind":"bool_literal","value":"false"}}`,
		`{"id":"x","kind":"print","value":{"id":"e","kind":"variable","symbolId":"s","value":true}}`,
	} {
		var statement Statement
		if err := json.Unmarshal([]byte(data), &statement); err == nil {
			t.Errorf("accepted malformed union: %s", data)
		}
	}
	for _, s := range []Statement{{ID: "x", Kind: "make_channel", SymbolID: "ch"}, {ID: "x", Kind: "receive", ChannelSymbolID: "ch"}, {ID: "x", Kind: "print", Value: &Expr{ID: "e", Kind: "bool_literal", Value: false}}} {
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		var roundtrip Statement
		if err = json.Unmarshal(data, &roundtrip); err != nil {
			t.Fatalf("cannot decode emitted union %s: %v", data, err)
		}
		if s.Kind == "make_channel" && !strings.Contains(string(data), `"capacity":0`) {
			t.Fatalf("capacity zero missing: %s", data)
		}
		if s.Kind == "receive" && !strings.Contains(string(data), `"bindings":[]`) {
			t.Fatalf("empty bindings missing: %s", data)
		}
	}
}
func TestUnknownProgramVersion(t *testing.T) {
	data, _ := json.Marshal(generatorFixture())
	data = []byte(strings.Replace(string(data), `"schemaVersion":"1.0"`, `"schemaVersion":"2.0"`, 1))
	var p Program
	err := json.Unmarshal(data, &p)
	var version *VersionError
	if !errors.As(err, &version) {
		t.Fatalf("expected VersionError, got %v", err)
	}
}
