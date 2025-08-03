package uv

import (
	_ "embed"
	"fmt"
	"os"
	"path"

	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

//go:embed uv.so
var uv_lib []byte

const (
	LIB_PATH = "/var/sandbox/sandbox-uv"
	LIB_NAME = "uv.so"
)

func init() {
	releaseLibBinary(true)
}

func releaseLibBinary(force_remove_old_lib bool) {
	log.Info("initializing uv runner environment...")
	// remove the old lib
	if _, err := os.Stat(path.Join(LIB_PATH, LIB_NAME)); err == nil {
		if force_remove_old_lib {
			err := os.Remove(path.Join(LIB_PATH, LIB_NAME))
			if err != nil {
				log.Panic(fmt.Sprintf("failed to remove %s", path.Join(LIB_PATH, LIB_NAME)))
			}

			// write the new lib
			err = os.MkdirAll(LIB_PATH, 0755)
			if err != nil {
				log.Panic(fmt.Sprintf("failed to create %s", LIB_PATH))
			}
			err = os.WriteFile(path.Join(LIB_PATH, LIB_NAME), uv_lib, 0755)
			if err != nil {
				log.Panic(fmt.Sprintf("failed to write %s", path.Join(LIB_PATH, LIB_NAME)))
			}
		}
	} else {
		err = os.MkdirAll(LIB_PATH, 0755)
		if err != nil {
			log.Panic(fmt.Sprintf("failed to create %s", LIB_PATH))
		}
		err = os.WriteFile(path.Join(LIB_PATH, LIB_NAME), uv_lib, 0755)
		if err != nil {
			log.Panic(fmt.Sprintf("failed to write %s", path.Join(LIB_PATH, LIB_NAME)))
		}
		log.Info("uv runner environment initialized")
	}
}

func checkLibAvaliable() bool {
	if _, err := os.Stat(path.Join(LIB_PATH, LIB_NAME)); err != nil {
		return false
	}

	return true
}
