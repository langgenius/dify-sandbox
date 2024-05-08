package types

import "encoding/json"

type RunnerOptions struct {
	EnableNetwork bool `json:"enable_network"`
}

func (r *RunnerOptions) Json() string {
	b, _ := json.Marshal(r)
	return string(b)
}
