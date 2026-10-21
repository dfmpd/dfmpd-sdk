package playback

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDTOValidation(t *testing.T) {
	cases := []struct {
		name, valid string
		newValue    func() any
		required    []string
		bad         map[string][]any
	}{
		{"request", `{"jsonrpc":"2.0","id":"1","method":"playback.play","params":{},"deadline_unix_ms":100}`, func() any { return new(Request) }, []string{"jsonrpc", "id", "method", "params", "deadline_unix_ms"}, map[string][]any{"jsonrpc": {"1.0", 2}, "id": {0, "0", "01", ""}, "method": {0, ""}, "params": {[]any{}, false}, "deadline_unix_ms": {0, -1, 1.5, "100"}}},
		{"response", `{"jsonrpc":"2.0","id":"1","result":{"accepted":true}}`, func() any { return new(Response) }, []string{"jsonrpc", "id", "result"}, map[string][]any{"jsonrpc": {"1.0"}, "id": {1, "0"}, "result": {false, []any{}}}},
		{"rpc-error", `{"code":-32000,"message":"error","data":{"code":"rejected","message":"no"}}`, func() any { return new(RPCError) }, []string{"code", "message"}, map[string][]any{"code": {0, "-32000", 1.5}, "message": {0, ""}, "data": {nil, false, map[string]any{"code": "unknown", "message": "no"}}}},
		{"initialization", `{"protocol_version":1}`, func() any { return new(Initialization) }, []string{"protocol_version"}, map[string][]any{"protocol_version": {0, 2, "1", true}}},
		{"capabilities", `{"protocol_version":1,"kind":"playback","name":"test","operations":["play","pause","resume","stop","seek","status"],"inputs":["local-file"]}`, func() any { return new(Capabilities) }, []string{"protocol_version", "kind", "name", "operations", "inputs"}, map[string][]any{"protocol_version": {2}, "kind": {"source"}, "name": {"", 1}, "operations": {[]any{}, []any{nil}, "play"}, "inputs": {[]any{"url"}, true}}},
		{"accepted", `{"accepted":true}`, func() any { return new(Accepted) }, []string{"accepted"}, map[string][]any{"accepted": {false, 1, "true"}}},
		{"input", `{"location":"/audio.flac"}`, func() any { return new(Input) }, []string{"location"}, map[string][]any{"location": {0, ""}}},
		{"seek", `{"seconds":0}`, func() any { return new(SeekParams) }, []string{"seconds"}, map[string][]any{"seconds": {-1, "0", true}}},
		{"status", `{"state":"idle","idle":false,"paused":false,"position_seconds":0}`, func() any { return new(Status) }, []string{"state", "idle", "paused"}, map[string][]any{"state": {"unknown", "", 0}, "idle": {"false", 0}, "paused": {"false", 0}, "position_seconds": {-1, "0"}, "failure": {false, map[string]any{"code": "rejected"}}}},
		{"error", `{"code":"timed_out","message":"expired"}`, func() any { return new(Error) }, []string{"code", "message"}, map[string][]any{"code": {"unknown", "", 0}, "message": {0, ""}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Decode([]byte(tc.valid), tc.newValue()); err != nil {
				t.Fatal(err)
			}
			fresh := func() map[string]any {
				var m map[string]any
				if err := json.Unmarshal([]byte(tc.valid), &m); err != nil {
					t.Fatal(err)
				}
				return m
			}
			check := func(m map[string]any, wantValid bool) {
				t.Helper()
				b, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				if err := Decode(b, tc.newValue()); (err == nil) != wantValid {
					t.Fatalf("valid=%v: %s -> %v", wantValid, b, err)
				}
			}
			m := fresh()
			m["future_field"] = map[string]any{"nested": []any{nil, false, 123}}
			check(m, true)
			for _, key := range tc.required {
				m = fresh()
				delete(m, key)
				check(m, false)
				m = fresh()
				m[key] = nil
				check(m, false)
			}
			for key, values := range tc.bad {
				for _, value := range values {
					m = fresh()
					m[key] = value
					check(m, false)
				}
			}
			for _, bad := range []string{"null", "[]", "true", "1", `"text"`, tc.valid + " {}"} {
				if err := Decode([]byte(bad), tc.newValue()); err == nil {
					t.Fatalf("accepted %s", bad)
				}
			}
		})
	}
}

func TestNestedExtensionsAndResponseShape(t *testing.T) {
	var r Response
	valid := `{"jsonrpc":"2.0","id":"1","future":true,"error":{"code":-32000,"message":"failed","future":{},"data":{"code":"unavailable","message":"gone","future":[]}}}`
	if err := Decode([]byte(valid), &r); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`"result":{},"error":null`, `"error":null`, `"result":null`, `"error":{"code":-1}`, `"unused":true`} {
		if err := Decode([]byte(`{"jsonrpc":"2.0","id":"1",`+body+`}`), &r); err == nil {
			t.Fatal(body)
		}
	}
	var s Status
	if err := Decode([]byte(`{"state":"idle","idle":true,"paused":false,"position_seconds":null,"failure":null,"future":true}`), &s); err != nil {
		t.Fatal(err)
	}
	if err := Decode([]byte(`{"state":"failed","idle":false,"paused":false,"failure":{"code":"media_failed","message":"bad file","future":true}}`), &s); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{`{"seconds":1e999}`, `{"seconds":NaN}`} {
		if err := Decode([]byte(invalid), new(SeekParams)); err == nil {
			t.Fatal(invalid)
		}
	}
	if _, err := Frame(map[string]string{"too_large": strings.Repeat("x", MaxFrame)}); err == nil {
		t.Fatal("oversized frame accepted")
	}
}
