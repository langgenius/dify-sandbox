package main

import (
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
)

const python_script = `def foo(a, b):
	return a + b
print(foo(1, 2))
`

func main() {
	runner := python.PythonRunner{}
	runner.Run(python_script, time.Minute, nil)
}
