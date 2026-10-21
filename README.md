# DFMPD Playback SDK (experimental)

Module: `github.com/dfmpd/dfmpd-sdk`. This unpublished module contains only the
Playback PoC communication boundary. It uses the Go standard library and has
no dependency on the daemon, its internal packages, or mpv.

`playback` provides wire DTOs, protocol version/capabilities, portable errors,
validation, framing, and a plugin-side stdio server. Implement `Backend` and
call `Serve` with owned stdin/stdout streams and an implementation name.
The executable must exit when Serve returns. stderr is for diagnostics.
Backend contexts/errors are local Go calls; only DTOs cross the wire.
Process launch, supervision, RPC proxy, and Core conversions belong to the host.

## Protocol

Experimental version 1 uses newline-delimited JSON-RPC 2.0 envelopes, up to
64 KiB including LF. This restricted profile has no batches, notifications,
or events. It is separate from DFMPD client IPC and mpv JSON IPC.

Requests require `jsonrpc`, a positive decimal string `id`, `method`, object
`params`, and a positive `deadline_unix_ms`. Responses echo the ID and contain
exactly one object `result` or `error`. Initialize with
`plugin.initialize` / `{"protocol_version":1}` before Playback operations.
Capabilities require version 1, kind `playback`, a nonempty name, all six
operations, and `local-file` input support.

Supported methods are `playback.play` (`location`), `playback.pause`,
`playback.resume`, `playback.stop`, `playback.seek` (`seconds`),
`playback.status`, and `plugin.shutdown`. Other operation parameters are empty
objects. Mutations return `{"accepted":true}`; this is acceptance, not proof
of audible playback. Status requires `state`, `idle`, and `paused`; optional
`position_seconds` and `failure` may be absent or null.

Within the supported version, unknown JSON fields are ignored. Missing/null
required fields, wrong types, and invalid values are rejected, including
negative seek positions and unknown state/error codes. False and zero remain
valid values where appropriate. Malformed envelopes terminate the session;
invalid operation parameters return a portable error.

Startup and RPC deadlines are bounded. A timeout or lost reply may leave an
operation's outcome unknown: the host must not automatically replay mutations.
There is no automatic restart, plugin manager, Source contract, or stable
public SDK promise in this PoC.

## Standalone validation

```sh
GOWORK=off go build ./...
GOWORK=off go test ./...
GOWORK=off go list -deps ./...
```

The daemon's development checkout uses a local `replace` pointing at this
directory. This module does not use a reverse replace or a workspace dependency
on the daemon. It is an independent Git repository; changes here are not tracked
by the parent repository, whose `workspace/` directory is ignored.

Code extracted from DFMPD retains its AGPL-3.0 license; see LICENSE.
