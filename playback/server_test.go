package playback

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeBackend struct {
	calls    []string
	paused   bool
	position float64
	closed   bool
}

func (f *fakeBackend) Play(context.Context, Input) error {
	f.calls = append(f.calls, "play")
	return nil
}
func (f *fakeBackend) Pause(context.Context) error {
	f.calls = append(f.calls, "pause")
	f.paused = true
	return nil
}
func (f *fakeBackend) Resume(context.Context) error {
	f.calls = append(f.calls, "resume")
	f.paused = false
	return nil
}
func (f *fakeBackend) Stop(context.Context) error { f.calls = append(f.calls, "stop"); return nil }
func (f *fakeBackend) Seek(_ context.Context, n float64) error {
	f.calls = append(f.calls, "seek")
	f.position = n
	return nil
}
func (f *fakeBackend) Status(context.Context) (Status, error) {
	f.calls = append(f.calls, "status")
	p := f.position
	return Status{State: Playing, Paused: f.paused, Position: &p}, nil
}
func (f *fakeBackend) Close() error { f.closed = true; return nil }

func TestServePlayback(t *testing.T) {
	in, hostWrite := io.Pipe()
	hostRead, out := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	backend := new(fakeBackend)
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, in, out, "test-plugin", func(context.Context) (Backend, error) { return backend, nil })
	}()
	var once sync.Once
	finish := func() {
		once.Do(func() {
			cancel()
			hostWrite.Close()
			hostRead.Close()
			if err := <-done; err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(finish)
	r := bufio.NewReader(hostRead)
	id := 0
	call := func(method, params string, result any) *RPCError {
		t.Helper()
		id++
		b, err := Frame(Request{JSONRPC: "2.0", ID: strconv.Itoa(id), Method: method, Params: json.RawMessage(params), Deadline: time.Now().Add(3 * time.Second).UnixMilli()})
		if err != nil {
			t.Fatal(err)
		}
		// Forward-compatible extension at the request envelope, too.
		var envelope map[string]any
		json.Unmarshal(b, &envelope)
		envelope["future"] = true
		b, _ = Frame(envelope)
		if _, err := hostWrite.Write(b); err != nil {
			t.Fatal(err)
		}
		line, err := r.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var reply Response
		if err := Decode(line, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.ID != strconv.Itoa(id) {
			t.Fatal("wrong reply ID")
		}
		if reply.Error == nil && result != nil {
			if err := Decode(reply.Result, result); err != nil {
				t.Fatal(err)
			}
		}
		return reply.Error
	}
	var caps Capabilities
	if e := call("plugin.initialize", `{"protocol_version":1,"future":true}`, &caps); e != nil || caps.Name != "test-plugin" {
		t.Fatalf("init: %+v %+v", caps, e)
	}
	for _, tc := range []struct{ method, params string }{
		{"playback.play", `{"location":"/song.flac","future":{}}`},
		{"playback.pause", `{"future":false}`},
		{"playback.resume", `{}`},
		{"playback.seek", `{"seconds":0,"future":null}`},
		{"playback.stop", `{}`},
	} {
		var ack Accepted
		if e := call(tc.method, tc.params, &ack); e != nil {
			t.Fatalf("%s: %+v", tc.method, e)
		}
	}
	var status Status
	if e := call("playback.status", `{}`, &status); e != nil || status.Paused || status.Position == nil || *status.Position != 0 {
		t.Fatalf("status=%+v error=%+v", status, e)
	}
	for _, bad := range []string{`{}`, `{"seconds":null}`, `{"seconds":"0"}`, `{"seconds":-1}`} {
		if e := call("playback.seek", bad, nil); e == nil || e.Data == nil || e.Data.Code != InvalidInput {
			t.Fatalf("invalid params accepted: %s %+v", bad, e)
		}
	}
	var ack Accepted
	if e := call("plugin.shutdown", `{"future":true}`, &ack); e != nil {
		t.Fatal(e)
	}
	// Observe server completion before inspecting the backend's state.
	err := <-done
	once.Do(func() { cancel(); hostRead.Close(); hostWrite.Close() })
	if err != nil {
		t.Fatal(err)
	}
	if !backend.closed || strings.Join(backend.calls, ",") != "play,pause,resume,seek,stop,status" {
		t.Fatalf("backend=%+v", backend)
	}
}

type outputBuffer struct{ bytes.Buffer }

func (*outputBuffer) Close() error { return nil }

func TestServeRejectsInvalidRequestBeforeOpening(t *testing.T) {
	for _, input := range []string{
		`{"jsonrpc":"2.0","id":"1","method":"plugin.initialize","deadline_unix_ms":100}`, // missing params
		`{"jsonrpc":"2.0","id":"1","method":"plugin.initialize","params":{},"deadline_unix_ms":null}`,
		`[]`, `null`, `{"jsonrpc":`,
	} {
		opened := false
		err := Serve(context.Background(), io.NopCloser(strings.NewReader(input+"\n")), new(outputBuffer), "test", func(context.Context) (Backend, error) { opened = true; return nil, errors.New("should not open") })
		if err == nil || opened {
			t.Fatalf("input=%s opened=%v err=%v", input, opened, err)
		}
	}
}
