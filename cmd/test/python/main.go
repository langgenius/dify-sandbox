package main

import (
	"fmt"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/internal/static"
)

func main() {
	static.InitConfig("conf/config.yaml")
	python.PreparePythonDependenciesEnv()
	resp := service.RunPython3Code(`import httpx
print(httpx.get("https://www.bilibili.com").text)`,
		``,
		&types.RunnerOptions{
			EnableNetwork: true,
			Dependencies:  []types.Dependency{},
		})

	fmt.Println(resp.Data)
}
