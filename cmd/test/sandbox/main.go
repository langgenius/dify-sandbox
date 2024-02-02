package main

import (
	"fmt"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

const python_script = `def foo(a, b):
	return a + b
print(foo(1, 2))

import json
import os
print(json.dumps({"a": 1, "b": 2}))
`

func main() {
	runner := python.PythonRunner{}
	stdout, stderr, done, err := runner.Run(python_script, time.Minute, nil)
	if err != nil {
		log.Panic("failed to run python script: %v", err)
	}

	for {
		select {
		case <-done:
			fmt.Println("done")
			return
		case out := <-stdout:
			fmt.Print(string(out))
		case err := <-stderr:
			fmt.Print(string(err))
		}
	}
}
