package playback

import (
	"encoding/json"
	"errors"
	"slices"
	"time"
)

const (
	Version         = 1
	MaxFrame        = 64 * 1024 // including LF
	StartupTimeout  = 5 * time.Second
	RPCTimeout      = 3 * time.Second
	ShutdownTimeout = 3 * time.Second
)

func DefaultCapabilities(name string) Capabilities {
	return Capabilities{Version: Version, Kind: "playback", Name: name,
		Operations: []string{"play", "pause", "resume", "stop", "seek", "status"}, Inputs: []string{"local-file"}}
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	// Same-host PoC extension: a queued operation must not start after its
	// deadline. This is not a remote-clock or external SDK contract.
	Deadline int64 `json:"deadline_unix_ms"`
}
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    *Error `json:"data,omitempty"`
}
type Initialization struct {
	Version int `json:"protocol_version"`
}
type Capabilities struct {
	Version    int      `json:"protocol_version"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name"`
	Operations []string `json:"operations"`
	Inputs     []string `json:"inputs"`
}
type Accepted struct {
	Accepted bool `json:"accepted"`
}

// Validate checks the capabilities needed by the current local-file Playback host.
func (c Capabilities) Validate() error {
	if c.Version != Version || c.Kind != "playback" || c.Name == "" || !slices.Contains(c.Inputs, "local-file") {
		return errors.New("incompatible playback capabilities")
	}
	for _, op := range DefaultCapabilities(c.Name).Operations {
		if !slices.Contains(c.Operations, op) {
			return errors.New("missing playback operation")
		}
	}
	for _, list := range [][]string{c.Operations, c.Inputs} {
		for _, item := range list {
			if item == "" {
				return errors.New("empty capability")
			}
		}
	}
	return nil
}

// Decode accepts additive JSON fields. DTO UnmarshalJSON methods enforce required
// fields and values, including nested objects. This function accepts objects only.
func Decode(data []byte, v any) error {
	if _, err := object(data); err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
func Frame(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b)+1 > MaxFrame {
		return nil, errors.New("plugin frame exceeds limit")
	}
	return append(b, '\n'), nil
}
func unavailable(message string) error {
	return &Error{Code: Unavailable, Message: message}
}

func ValidCode(code Code) bool {
	switch code {
	case InvalidInput, Unavailable, Rejected, TimedOut, Canceled, Closed, MediaFailed:
		return true
	}
	return false
}

type SeekParams struct {
	Seconds float64 `json:"seconds"`
}
