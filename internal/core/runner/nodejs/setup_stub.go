//go:build !linux

package nodejs

import (
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

const (
	LIB_PATH     = "/var/sandbox/sandbox-nodejs"
	LIB_NAME     = "nodejs.so"
	PROJECT_NAME = "nodejs-project"
)

func init() {
	log.Warn("Node.js native runner is only supported on Linux. Please use microsandbox backend on this platform.")
}

func releaseLibBinary() {
	log.Warn("Cannot release Node.js lib binary on non-Linux platform")
}

func checkLibAvaliable() bool {
	return false
}
