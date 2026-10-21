package playback

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Serve owns the streams and the adapter returned by open. stdout is exclusively
// protocol output. The executable must exit when Serve returns, including when
// an uncooperative backend exceeds a deadline.
func Serve(ctx context.Context, in io.ReadCloser, out io.WriteCloser, name string, open func(context.Context) (Backend, error)) (err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer in.Close()
	defer out.Close()
	if err := DefaultCapabilities(name).Validate(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { in.Close(); out.Close() })
	defer stop()
	var adapter Backend
	defer func() {
		if adapter != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
			defer cancel()
			err = errors.Join(err, bounded(cleanup, adapter.Close))
		}
	}()
	reader := bufio.NewReaderSize(in, MaxFrame)
	// An uninitialized process may not wait indefinitely for a host.
	startup, endStartup := context.WithTimeout(ctx, StartupTimeout)
	defer endStartup()
	startupStop := context.AfterFunc(startup, func() { in.Close() })
	defer startupStop()
	for {
		line, e := reader.ReadSlice('\n')
		if e != nil {
			if e == io.EOF && len(line) == 0 {
				return nil
			}
			return fmt.Errorf("plugin request stream: %w", e)
		}
		var req Request
		if Decode(line, &req) != nil || req.JSONRPC != "2.0" || req.ID == "" || req.Deadline <= 0 {
			return errors.New("invalid plugin request")
		}
		limit := RPCTimeout
		if req.Method == "plugin.initialize" {
			limit = StartupTimeout
		}
		deadline := time.UnixMilli(req.Deadline)
		if max := time.Now().Add(limit); deadline.After(max) {
			deadline = max
		}
		callCtx, endCall := context.WithDeadline(ctx, deadline)
		var value any = Accepted{Accepted: true}
		var callErr error
		shutdown := false
		switch {
		case callCtx.Err() != nil:
			callErr = callCtx.Err()
		case req.Method == "plugin.initialize":
			var init Initialization
			if adapter != nil || Decode(req.Params, &init) != nil || init.Version != Version {
				callErr = &Error{Code: Rejected, Message: "unsupported or repeated Initialization"}
				break
			}
			// Publish the new adapter only on the protocol goroutine. If startup
			// expires, the late factory owns cleanup of anything it created.
			type opened struct {
				a   Backend
				err error
			}
			ready := make(chan opened)
			go func() {
				a, e := open(callCtx)
				select {
				case ready <- opened{a, e}:
				case <-callCtx.Done():
					if a != nil {
						_ = a.Close()
					}
				}
			}()
			select {
			case result := <-ready:
				adapter, callErr = result.a, result.err
			case <-callCtx.Done():
				callErr = callCtx.Err()
			}
			if callErr == nil && adapter == nil {
				callErr = unavailable("no playback adapter")
			}
			if callErr == nil {
				startupStop()
				endStartup()
				value = DefaultCapabilities(name)
			}
		case adapter == nil:
			callErr = &Error{Code: Rejected, Message: "plugin is not initialized"}
		case req.Method == "plugin.shutdown":
			if Decode(req.Params, &struct{}{}) != nil {
				callErr = invalidParams()
				break
			}
			callErr = bounded(callCtx, adapter.Close)
			adapter = nil // Close was attempted exactly once, including timeout.
			shutdown = true
		default:
			value, callErr = dispatch(callCtx, adapter, req)
		}
		endCall()
		reply := Response{JSONRPC: "2.0", ID: req.ID}
		if callErr != nil {
			var e *Error
			if !errors.As(callErr, &e) {
				code := Unavailable
				if errors.Is(callErr, context.DeadlineExceeded) {
					code = TimedOut
				}
				if errors.Is(callErr, context.Canceled) {
					code = Canceled
				}
				e = &Error{Code: code, Message: callErr.Error()}
			}
			if e == nil || e.Validate() != nil {
				e = &Error{Code: Unavailable, Message: "invalid backend error"}
			}
			reply.Error = &RPCError{Code: -32000, Message: "Playback operation failed", Data: e}
		} else {
			reply.Result, err = json.Marshal(value)
			if err != nil {
				return err
			}
		}
		data, e := Frame(reply)
		if e != nil {
			return e
		}
		writeCtx, endWrite := context.WithTimeout(ctx, RPCTimeout)
		e = bounded(writeCtx, func() error { _, e := out.Write(data); return e })
		endWrite()
		if e != nil {
			return e
		}
		if shutdown {
			return callErr
		}
		// A backend that failed to finish within its budget is not safe to reuse.
		if errors.Is(callErr, context.DeadlineExceeded) || errors.Is(callErr, context.Canceled) {
			return callErr
		}
	}
}

func invalidParams() error {
	return &Error{Code: InvalidInput, Message: "invalid plugin parameters"}
}
func bounded(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func dispatch(ctx context.Context, a Backend, req Request) (any, error) {
	var fn func() error
	switch req.Method {
	case "playback.play":
		var input Input
		if Decode(req.Params, &input) != nil || input.Location == "" {
			return nil, invalidParams()
		}
		fn = func() error { return a.Play(ctx, input) }
	case "playback.seek":
		var p SeekParams
		if Decode(req.Params, &p) != nil {
			return nil, invalidParams()
		}
		fn = func() error { return a.Seek(ctx, p.Seconds) }
	case "playback.pause", "playback.resume", "playback.stop", "playback.status":
		if Decode(req.Params, &struct{}{}) != nil {
			return nil, invalidParams()
		}
		switch req.Method {
		case "playback.pause":
			fn = func() error { return a.Pause(ctx) }
		case "playback.resume":
			fn = func() error { return a.Resume(ctx) }
		case "playback.stop":
			fn = func() error { return a.Stop(ctx) }
		case "playback.status":
			// The channel owns the snapshot even when the caller times out.
			type observed struct {
				status Status
				err    error
			}
			ch := make(chan observed, 1)
			go func() { s, err := a.Status(ctx); ch <- observed{s, err} }()
			select {
			case r := <-ch:
				if r.err == nil {
					r.err = r.status.Validate()
				}
				return r.status, r.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	default:
		return nil, &Error{Code: Rejected, Message: "unknown plugin method"}
	}
	return Accepted{Accepted: true}, bounded(ctx, fn)
}
