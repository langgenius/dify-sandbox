package main

import (
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"os"
)

func main() {
	if err := isolation.WorkerMain(); err != nil {
		fmt.Fprintln(os.Stderr, "request isolation setup failed:", err)
		os.Exit(125)
	}
}
