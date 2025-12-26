package main

import (
	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/core/runner/uv"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

func main() {
	static.InitConfig("conf/config.yaml")

	err := python.PreparePythonDependenciesEnv()
	if err != nil {
		log.Panic("failed to initialize python dependencies sandbox: %v", err)
	}

	log.Info("Python dependencies initialized successfully")

	err = uv.PrepareUvDependenciesEnv()
	if err != nil {
		log.Panic("failed to initialize uv dependencies sandbox: %v", err)
	}

	log.Info("UV dependencies initialized successfully")
}
