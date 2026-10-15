package main

import (
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"os"
)

func main() {
	if network.FrontendMain() != nil {
		os.Exit(125)
	}
}
