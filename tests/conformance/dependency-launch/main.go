// Build-time probe helper: exposes the real native bootstrap to the disposable
// component probe. It is outside the user root and is not a service endpoint.
package main

import (
	"encoding/json"
	"github.com/langgenius/dify-sandbox/internal/service"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(2)
	}
	switch os.Args[1] {
	case "python3":
		json.NewEncoder(os.Stdout).Encode(service.PythonIsolatedLaunch("/usr/bin/python3"))
	case "nodejs":
		json.NewEncoder(os.Stdout).Encode(service.NodeIsolatedLaunch("/usr/bin/node"))
	default:
		os.Exit(2)
	}
}
