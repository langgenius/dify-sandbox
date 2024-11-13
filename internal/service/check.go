package service

import (
	"errors"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
)

var (
	ErrNetworkDisabled = errors.New("network is disabled, please enable it in the configuration")
)

func checkOptions(options *types.RunnerOptions) error {
	configuration := static.GetDifySandboxGlobalConfigurations()

	if options.EnableNetwork && !configuration.EnableNetwork {
		return ErrNetworkDisabled
	}

	return nil
}
