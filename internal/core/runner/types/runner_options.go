package types

import (
	"encoding/json"
	"io"
)

type Dependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type RunnerOptions struct {
	EnableNetwork bool                 `json:"enable_network"`
	InputFiles    map[string]io.Reader `json:"-"` // Map filename -> reader
	FetchFiles    []string             `json:"fetch_files"`
	// OutputHandler is called for each file in FetchFiles before cleanup.
	// Args: filename, localPath. Returns: fileId (or reference), error.
	OutputHandler func(string, string) (string, error) `json:"-"`
}

func (r *RunnerOptions) Json() string {
	b, _ := json.Marshal(r)
	return string(b)
}
