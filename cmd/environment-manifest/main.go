package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/environment"
	"os"
	"runtime"
)

func main() {
	root := flag.String("root", "/environment-root", "trusted build root")
	verify := flag.String("verify", "", "expected artifact digest")
	flag.Parse()
	var m environment.EnvironmentManifest
	var err error
	if *verify != "" {
		m, err = environment.LoadAndVerify(*root, *verify)
	} else {
		m, err = environment.BuildManifest(*root, environment.EnvironmentManifest{PythonVersion: "3.12.12", NodeVersion: "20.11.1", JinjaVersion: "3.1.6", Architecture: runtime.GOARCH})
		if err == nil {
			err = environment.WriteManifest(*root, m)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(m)
}
