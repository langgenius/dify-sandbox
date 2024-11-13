package nodejs

import (
	"embed"
	"fmt"
	"os"
	"path"

	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

const (
	LIB_PATH     = "/var/sandbox/sandbox-nodejs"
	LIB_NAME     = "nodejs.so"
	PROJECT_NAME = "nodejs-project"
)

//go:embed nodejs.so
var nodejs_lib []byte

//go:embed dependens
var nodejs_dependens embed.FS // it's a directory

func init() {
	releaseLibBinary()
}

func releaseLibBinary() {
	log.Info("initializing nodejs runner environment...")
	os.RemoveAll(LIB_PATH)
	os.Remove(LIB_PATH)

	err := os.MkdirAll(LIB_PATH, 0755)
	if err != nil {
		log.Panic(fmt.Sprintf("failed to create %s", LIB_PATH))
	}
	err = os.WriteFile(path.Join(LIB_PATH, LIB_NAME), nodejs_lib, 0755)
	if err != nil {
		log.Panic(fmt.Sprintf("failed to write %s", path.Join(LIB_PATH, PROJECT_NAME)))
	}

	// copy the nodejs project into /tmp/sandbox-nodejs-project
	err = os.MkdirAll(path.Join(LIB_PATH, PROJECT_NAME), 0755)
	if err != nil {
		log.Panic(fmt.Sprintf("failed to create %s", path.Join(LIB_PATH, PROJECT_NAME)))
	}

	// copy the nodejs project into /tmp/sandbox-nodejs-project
	var recursively_copy func(src string, dst string) error
	recursively_copy = func(src string, dst string) error {
		entries, err := nodejs_dependens.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			src_path := src + "/" + entry.Name()
			dst_path := dst + "/" + entry.Name()
			if entry.IsDir() {
				err = os.Mkdir(dst_path, 0755)
				if err != nil {
					return err
				}
				err = recursively_copy(src_path, dst_path)
				if err != nil {
					return err
				}
			} else {
				data, err := nodejs_dependens.ReadFile(src_path)
				if err != nil {
					return err
				}
				err = os.WriteFile(dst_path, data, 0755)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}

	err = recursively_copy("dependens", path.Join(LIB_PATH, PROJECT_NAME))
	if err != nil {
		log.Panic("failed to copy nodejs project")
	}
	log.Info("nodejs runner environment initialized")
}

func checkLibAvaliable() bool {
	if _, err := os.Stat(path.Join(LIB_PATH, LIB_NAME)); err != nil {
		return false
	}

	return true
}
