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
import time
print(json.dumps({"a": 1, "b": 2}), flush=True)
time.sleep(3)
`

func main() {
	runner := python.PythonRunner{}
	stdout, stderr, done, err := runner.Run(python_script, time.Second*10, nil)
	if err != nil {
		log.Panic("failed to run python script: %v", err)
	}

	for {
		select {
		case <-done:
			return
		case out := <-stdout:
			fmt.Println(string(out))
		case err := <-stderr:
			fmt.Println(string(err))
		}
	}
}
