package integrationtests_test

import (
	"os"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/core/runner/uv"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

func init() {
	static.InitConfig("conf/config.yaml")

	err := python.PreparePythonDependenciesEnv()
	if err != nil {
		log.Panic("failed to initialize python dependencies sandbox: %v", err)
	}

	err = uv.PrepareUvDependenciesEnv()
	if err != nil {
		log.Panic("failed to initialize uv dependencies sandbox: %v", err)
	}

	listSandboxFiles()
}

func listSandboxFiles() {
	dir := "/var/sandbox/sandbox-uv/"
	files, err := os.ReadDir(dir)
	if err != nil {
		log.Panic("failed to read directory %s: %v", dir, err)
	}

	for _, file := range files {
		log.Info("File: %s", file.Name())
	}
}
