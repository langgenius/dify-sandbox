package main

import (
	"fmt"
	"log/slog"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/static"
)

func main() {
	err := static.InitConfig("conf/config.yaml")
	if err != nil {
		slog.Error("failed to initialize config", "err", err)
		panic(fmt.Sprintf("failed to initialize config: %v", err))
	}

	err = python.PreparePythonDependenciesEnv()
	if err != nil {
		slog.Error("failed to initialize python dependencies sandbox", "err", err)
		panic(fmt.Sprintf("failed to initialize python dependencies sandbox: %v", err))
	}

	slog.Info("Python dependencies initialized successfully")
}
