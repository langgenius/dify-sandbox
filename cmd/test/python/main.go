package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/internal/static"
)

func main() {
	if err := static.InitConfig("conf/config.yaml"); err != nil {
		slog.Error("failed to initialize config", "err", err)
		panic(fmt.Sprintf("failed to initialize config: %v", err))
	}
	if err := python.PreparePythonDependenciesEnv(); err != nil {
		slog.Error("failed to initialize python dependencies sandbox", "err", err)
		panic(fmt.Sprintf("failed to initialize python dependencies sandbox: %v", err))
	}
	resp := service.RunPython3Code(context.Background(), `import json;print(json.dumps({"hello": "world"}))`,
		``,
		&types.RunnerOptions{
			EnableNetwork: true,
		})

	fmt.Println(resp.Data)
}
