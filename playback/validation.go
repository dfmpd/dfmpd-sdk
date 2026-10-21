package playback

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// Presence is checked separately from decoding so false/0 remain valid while
// absent or null mandatory fields cannot silently become Go zero values.
func object(data []byte, required ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("expected JSON object")
	}
	for _, key := range required {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, fmt.Errorf("missing or null %s", key)
		}
	}
	return fields, nil
}

func validID(id string) bool {
	n, err := strconv.ParseUint(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == id
}
func (r *Request) UnmarshalJSON(data []byte) error {
	if _, err := object(data, "jsonrpc", "id", "method", "params", "deadline_unix_ms"); err != nil {
		return err
	}
	type plain Request
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.JSONRPC != "2.0" || !validID(v.ID) || v.Method == "" || v.Deadline <= 0 {
		return errors.New("invalid request fields")
	}
	if _, err := object(v.Params); err != nil {
		return err
	}
	*r = Request(v)
	return nil
}
func (r *Response) UnmarshalJSON(data []byte) error {
	f, err := object(data, "jsonrpc", "id")
	if err != nil {
		return err
	}
	_, result := f["result"]
	_, failure := f["error"]
	if result == failure {
		return errors.New("expected exactly one result or error")
	}
	key := "result"
	if failure {
		key = "error"
	}
	if _, err := object(f[key]); err != nil {
		return err
	}
	type plain Response
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.JSONRPC != "2.0" || !validID(v.ID) {
		return errors.New("invalid response fields")
	}
	*r = Response(v)
	return nil
}
func (e *RPCError) UnmarshalJSON(data []byte) error {
	f, err := object(data, "code", "message")
	if err != nil {
		return err
	}
	if d, ok := f["data"]; ok {
		if _, err := object(d); err != nil {
			return err
		}
	}
	type plain RPCError
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.Code == 0 || v.Message == "" {
		return errors.New("invalid RPC error")
	}
	*e = RPCError(v)
	return nil
}
func (i *Initialization) UnmarshalJSON(data []byte) error {
	if _, err := object(data, "protocol_version"); err != nil {
		return err
	}
	type plain Initialization
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.Version != Version {
		return errors.New("unsupported protocol version")
	}
	*i = Initialization(v)
	return nil
}
func (c *Capabilities) UnmarshalJSON(data []byte) error {
	if _, err := object(data, "protocol_version", "kind", "name", "operations", "inputs"); err != nil {
		return err
	}
	type plain Capabilities
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	value := Capabilities(v)
	if err := value.Validate(); err != nil {
		return err
	}
	*c = value
	return nil
}
func (a *Accepted) UnmarshalJSON(data []byte) error {
	if _, err := object(data, "accepted"); err != nil {
		return err
	}
	type plain Accepted
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if !v.Accepted {
		return errors.New("command not acknowledged")
	}
	*a = Accepted(v)
	return nil
}
func (i *Input) UnmarshalJSON(data []byte) error {
	if _, err := object(data, "location"); err != nil {
		return err
	}
	type plain Input
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.Location == "" {
		return errors.New("empty location")
	}
	*i = Input(v)
	return nil
}
func (p *SeekParams) UnmarshalJSON(data []byte) error {
	if _, err := object(data, "seconds"); err != nil {
		return err
	}
	type plain SeekParams
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if !finiteSeconds(v.Seconds) {
		return errors.New("invalid seek seconds")
	}
	*p = SeekParams(v)
	return nil
}
func finiteSeconds(n float64) bool { return n >= 0 && !math.IsNaN(n) && !math.IsInf(n, 0) }
func (e Error) Validate() error {
	if !ValidCode(e.Code) || e.Message == "" {
		return errors.New("invalid playback error")
	}
	return nil
}
func (e *Error) UnmarshalJSON(data []byte) error {
	if _, err := object(data, "code", "message"); err != nil {
		return err
	}
	type plain Error
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	value := Error(v)
	if err := value.Validate(); err != nil {
		return err
	}
	*e = value
	return nil
}
func (s Status) Validate() error {
	switch s.State {
	case Idle, Loading, Playing, Paused, Failed:
	default:
		return errors.New("invalid playback state")
	}
	if s.Position != nil && !finiteSeconds(*s.Position) {
		return errors.New("invalid playback position")
	}
	if s.Failure != nil {
		return s.Failure.Validate()
	}
	return nil
}
func (s *Status) UnmarshalJSON(data []byte) error {
	if _, err := object(data, "state", "idle", "paused"); err != nil {
		return err
	}
	type plain Status
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	value := Status(v)
	if err := value.Validate(); err != nil {
		return err
	}
	*s = value
	return nil
}
