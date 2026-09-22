package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type VersionError struct{ Version string }

func (e *VersionError) Error() string { return fmt.Sprintf("unsupported schemaVersion %q", e.Version) }

// object checks each union's exact shape before decoding nested values. Null is
// allowed only where the contract explicitly gives it a meaning.
func object(data []byte, required, optional, nullable []string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("expected an object")
	}
	allowed, nulls := map[string]bool{}, map[string]bool{}
	for _, key := range append(required, optional...) {
		allowed[key] = true
	}
	for _, key := range nullable {
		nulls[key] = true
	}
	for key, value := range fields {
		if !allowed[key] {
			return nil, fmt.Errorf("unknown field %q", key)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !nulls[key] {
			return nil, fmt.Errorf("field %q cannot be null", key)
		}
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return nil, fmt.Errorf("missing field %q", key)
		}
	}
	return fields, nil
}
func decodeExact(data []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}
func (p *Program) UnmarshalJSON(data []byte) error {
	if _, err := object(data, []string{"schemaVersion", "id", "packageName", "entryFunctionId", "functions", "symbols"}, nil, nil); err != nil {
		return err
	}
	type plain Program
	var v plain
	if err := decodeExact(data, &v); err != nil {
		return err
	}
	if v.SchemaVersion != "1.0" {
		return &VersionError{Version: v.SchemaVersion}
	}
	*p = Program(v)
	return nil
}
func (f *Function) UnmarshalJSON(data []byte) error {
	if _, err := object(data, []string{"id", "kind", "parentFunctionId", "body"}, nil, []string{"parentFunctionId"}); err != nil {
		return err
	}
	type plain Function
	var v plain
	if err := decodeExact(data, &v); err != nil {
		return err
	}
	if v.Kind != "main" && v.Kind != "goroutine" {
		return fmt.Errorf("unknown function kind %q", v.Kind)
	}
	*f = Function(v)
	return nil
}
func (b *Block) UnmarshalJSON(data []byte) error {
	if _, err := object(data, []string{"id", "statements"}, nil, nil); err != nil {
		return err
	}
	type plain Block
	var v plain
	if err := decodeExact(data, &v); err != nil {
		return err
	}
	*b = Block(v)
	return nil
}
func (s *Symbol) UnmarshalJSON(data []byte) error {
	if _, err := object(data, []string{"id", "name", "type", "scopeId"}, nil, nil); err != nil {
		return err
	}
	type plain Symbol
	var v plain
	if err := decodeExact(data, &v); err != nil {
		return err
	}
	*s = Symbol(v)
	return nil
}
func (e *Expr) UnmarshalJSON(data []byte) error {
	var tag struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	fields := []string{"id", "kind"}
	switch tag.Kind {
	case "int_literal", "bool_literal":
		fields = append(fields, "value")
	case "variable":
		fields = append(fields, "symbolId")
	default:
		return fmt.Errorf("unknown expression kind %q", tag.Kind)
	}
	raw, err := object(data, fields, nil, nil)
	if err != nil {
		return err
	}
	var v Expr
	v.Kind = tag.Kind
	if err = json.Unmarshal(raw["id"], &v.ID); err != nil {
		return err
	}
	switch tag.Kind {
	case "int_literal":
		var x string
		err = json.Unmarshal(raw["value"], &x)
		v.Value = x
	case "bool_literal":
		var x bool
		err = json.Unmarshal(raw["value"], &x)
		v.Value = x
	case "variable":
		err = json.Unmarshal(raw["symbolId"], &v.SymbolID)
	}
	if err != nil {
		return err
	}
	*e = v
	return nil
}
func (e Expr) MarshalJSON() ([]byte, error) {
	switch e.Kind {
	case "int_literal", "bool_literal":
		return json.Marshal(struct {
			ID    string `json:"id"`
			Kind  string `json:"kind"`
			Value any    `json:"value"`
		}{e.ID, e.Kind, e.Value})
	case "variable":
		return json.Marshal(struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			SymbolID string `json:"symbolId"`
		}{e.ID, e.Kind, e.SymbolID})
	default:
		return nil, fmt.Errorf("unknown expression kind %q", e.Kind)
	}
}
func (s *Statement) UnmarshalJSON(data []byte) error {
	var tag struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	fields := []string{"id", "kind"}
	switch tag.Kind {
	case "make_channel":
		fields = append(fields, "symbolId", "capacity")
	case "let":
		fields = append(fields, "symbolId", "value")
	case "spawn":
		fields = append(fields, "functionId")
	case "send":
		fields = append(fields, "channelSymbolId", "value")
	case "receive":
		fields = append(fields, "channelSymbolId", "bindings")
	case "close":
		fields = append(fields, "channelSymbolId")
	case "print":
		fields = append(fields, "value")
	case "return":
	default:
		return fmt.Errorf("unknown statement kind %q", tag.Kind)
	}
	if _, err := object(data, fields, nil, nil); err != nil {
		return err
	}
	type plain Statement
	var v plain
	if err := decodeExact(data, &v); err != nil {
		return err
	}
	*s = Statement(v)
	return nil
}
func (s Statement) MarshalJSON() ([]byte, error) {
	m := map[string]any{"id": s.ID, "kind": s.Kind}
	switch s.Kind {
	case "make_channel":
		m["symbolId"] = s.SymbolID
		m["capacity"] = s.Capacity
	case "let":
		m["symbolId"] = s.SymbolID
		m["value"] = s.Value
	case "spawn":
		m["functionId"] = s.FunctionID
	case "send":
		m["channelSymbolId"] = s.ChannelSymbolID
		m["value"] = s.Value
	case "receive":
		m["channelSymbolId"] = s.ChannelSymbolID
		bindings := s.Bindings
		if bindings == nil {
			bindings = []*string{}
		}
		m["bindings"] = bindings
	case "close":
		m["channelSymbolId"] = s.ChannelSymbolID
	case "print":
		m["value"] = s.Value
	case "return":
	default:
		return nil, fmt.Errorf("unknown statement kind %q", s.Kind)
	}
	return json.Marshal(m)
}
