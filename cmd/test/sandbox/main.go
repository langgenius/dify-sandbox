package main

import (
	"fmt"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

const python_script = `
print(123)`

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
			if string(out) != "" {
				fmt.Println(string(out))
			}
		case err := <-stderr:
			if string(err) != "" {
				fmt.Println(string(err))
			}
		}
	}
}
