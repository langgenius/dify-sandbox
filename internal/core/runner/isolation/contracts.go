package isolation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"io"
)

type InvocationSpec struct {
	Version              int                  `json:"schema_version"`
	ExecutionID          string               `json:"execution_id"`
	InvocationDigest     string               `json:"invocation_digest"`
	Language             string               `json:"language"`
	EnvironmentDigest    string               `json:"environment_digest"`
	ProfileDigest        string               `json:"profile_digest"`
	BudgetReservationRef string               `json:"budget_reservation_ref"`
	Budget               Budget               `json:"budget"`
	Network              network.NetworkScope `json:"network_scope"`
}
type Payload struct {
	Code    string `json:"code"`
	Preload string `json:"preload"`
}
type InvocationRequest struct {
	Options  InvocationSpec `json:"options"`
	Code     string         `json:"code"`
	Preload  string         `json:"preload"`
	Language string         `json:"language"`
}
type Usage struct {
	WallMillis      int64 `json:"wall_millis"`
	CPUTimeMicros   int64 `json:"cpu_time_micros"`
	MemoryPeakBytes int64 `json:"memory_peak_bytes"`
}
type ExecutionResult struct {
	ExecutionID      string                  `json:"execution_id"`
	InvocationDigest string                  `json:"invocation_digest"`
	Status           string                  `json:"status"`
	FailureReason    string                  `json:"failure_reason,omitempty"`
	CleanupState     string                  `json:"cleanup_state"`
	Stdout           string                  `json:"stdout"`
	Stderr           string                  `json:"stderr"`
	ExitCode         int                     `json:"exit_code"`
	Usage            Usage                   `json:"usage"`
	NetworkReceipt   *network.NetworkReceipt `json:"network_receipt,omitempty"`
}

// Launch is supplied by the existing language runner, not by the HTTP caller.
type Launch struct {
	Executable      string
	Args            []string
	Bootstrap       string
	BootstrapPath   string
	ScopedBootstrap string
	ScopedArgs      []string
}
type Failure struct {
	Reason string
	Err    error
}

func (e *Failure) Error() string           { return e.Reason + ": " + e.Err.Error() }
func (e *Failure) Unwrap() error           { return e.Err }
func failure(reason, message string) error { return &Failure{reason, errors.New(message)} }

// InvocationDigest binds the entire spec (except this digest) to the exact
// canonical payload bytes. Reservation lookup is still required: a hash is not authority.
func InvocationDigest(spec InvocationSpec, payload []byte) string {
	spec.InvocationDigest = ""
	data, _ := json.Marshal(spec)
	h := sha256.New()
	h.Write(data)
	h.Write([]byte{0})
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}
func readBounded(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 || limit > 1<<62 {
		return nil, failure("input_limit", "invalid limit")
	}
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, failure("input_limit", "request exceeds limit")
	}
	return b, nil
}
func decodeStrict(data []byte, v any) error {
	// Reject duplicate object keys as well as unknown fields and trailing JSON.
	d := json.NewDecoder(bytes.NewReader(data))
	if err := checkJSONValue(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func checkJSONValue(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			k, ok := key.(string)
			if !ok || seen[k] {
				return fmt.Errorf("duplicate or invalid JSON key")
			}
			seen[k] = true
			if err := checkJSONValue(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := checkJSONValue(d); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	_, err = d.Token()
	return err
}
func DecodeInvocation(r io.Reader, limit int64) (InvocationRequest, error) {
	var req InvocationRequest
	b, err := readBounded(r, limit)
	if err != nil {
		return req, err
	}
	if err = decodeStrict(b, &req); err != nil {
		return req, err
	}
	if req.Options.Version != 1 || req.Language == "" || req.Language != req.Options.Language {
		return req, fmt.Errorf("Builder invocation envelope required")
	}
	return req, nil
}
