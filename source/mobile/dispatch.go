package mobile

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// encodeReply gives {"ok": result} or {"error": message}; bridge.js resolves or rejects
// the UI's promise with it.
func encodeReply(v any, err error) string {
	if err == nil {
		b, merr := json.Marshal(map[string]any{"ok": v})
		if merr == nil {
			return string(b)
		}
		err = merr
	}
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	return string(b)
}

var errType = reflect.TypeOf((*error)(nil)).Elem()

// dispatch calls target's exported method by name with a JSON array of arguments, the
// way Wails does for window.go.*: arguments are decoded into the parameter types, missing
// ones are zero values, and a trailing error result becomes the error.
func dispatch(target any, method, argsJSON string) (res any, err error) {
	if target == nil {
		return nil, errors.New("not started")
	}
	m := reflect.ValueOf(target).MethodByName(method)
	if !m.IsValid() {
		return nil, nil // like api.js: unknown methods resolve to null
	}
	var raw []json.RawMessage
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
			return nil, fmt.Errorf("bad arguments for %s: %w", method, err)
		}
	}
	t := m.Type()
	if t.IsVariadic() {
		return nil, fmt.Errorf("%s can't be called from the UI", method)
	}
	in := make([]reflect.Value, t.NumIn())
	for i := range in {
		p := reflect.New(t.In(i))
		if i < len(raw) && string(raw[i]) != "null" {
			if err := json.Unmarshal(raw[i], p.Interface()); err != nil {
				return nil, fmt.Errorf("bad argument %d for %s: %w", i+1, method, err)
			}
		}
		in[i] = p.Elem()
	}
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fmt.Errorf("%s failed: %v", method, r)
		}
	}()
	outs := m.Call(in)
	if n := len(outs); n > 0 && t.Out(n-1) == errType {
		if e := outs[n-1]; !e.IsNil() {
			return nil, e.Interface().(error)
		}
		outs = outs[:n-1]
	}
	if len(outs) == 0 {
		return nil, nil
	}
	return outs[0].Interface(), nil
}
