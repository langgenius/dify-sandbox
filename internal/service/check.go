package service

import (
	"errors"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
)

var (
	ErrNetworkDisabled            = errors.New("network is disabled, please enable it in the configuration")
	ErrCustomDependenciesDisabled = errors.New("custom dependencies are disabled, please enable it in the configuration")
)

func checkOptions(options *types.RunnerOptions) error {
	configuration := static.GetDifySandboxGlobalConfigurations()

	if options.EnableNetwork && !configuration.EnableNetwork {
		return ErrNetworkDisabled
	}

	if !configuration.EnableCustomDependencies && len(options.Dependencies) > 0 {
		return ErrCustomDependenciesDisabled
	}

	return nil
}
