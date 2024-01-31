package main

import (
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
)

func main() {
	runner := python.PythonRunner{}
	runner.Run("aaa", time.Minute, nil)
}
