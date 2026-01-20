package integrationtests_test

import (
	"fmt"
	"log/slog"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/static"
)

func init() {
	static.InitConfig("conf/config.yaml")

	err := python.PreparePythonDependenciesEnv()
	if err != nil {
		slog.Error("failed to initialize python dependencies sandbox", "err", err)
		panic(fmt.Sprintf("failed to initialize python dependencies sandbox: %v", err))
	}
}
