package types

import "encoding/json"

type Dependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type RunnerOptions struct {
	EnableNetwork bool `json:"enable_network"`
}

func (r *RunnerOptions) Json() string {
	b, _ := json.Marshal(r)
	return string(b)
}
