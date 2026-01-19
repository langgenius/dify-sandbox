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

	createDirectory(LIB_PATH)
	writeLibFile()
	createProjectDirectory()
	copyDependencies()
	log.Info("nodejs runner environment initialized")
}

func createDirectory(dirPath string) {
	err := os.MkdirAll(dirPath, 0755)
	if err != nil {
		log.Panic(fmt.Sprintf("failed to create %s", dirPath))
	}
}

func writeLibFile() {
	libPath := path.Join(LIB_PATH, LIB_NAME)
	err := os.WriteFile(libPath, nodejs_lib, 0755)
	if err != nil {
		log.Panic(fmt.Sprintf("failed to write %s", libPath))
	}
}

func createProjectDirectory() {
	projectPath := path.Join(LIB_PATH, PROJECT_NAME)
	err := os.MkdirAll(projectPath, 0755)
	if err != nil {
		log.Panic(fmt.Sprintf("failed to create %s", projectPath))
	}
}

func copyDependencies() {
	err := recursivelyCopy("dependens", path.Join(LIB_PATH, PROJECT_NAME))
	if err != nil {
		log.Panic("failed to copy nodejs project")
	}
}

func recursivelyCopy(src string, dst string) error {
	entries, err := nodejs_dependens.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := src + "/" + entry.Name()
		dstPath := dst + "/" + entry.Name()

		if entry.IsDir() {
			if err := copyDirectory(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyDirectory(srcPath, dstPath string) error {
	if err := os.Mkdir(dstPath, 0755); err != nil {
		return err
	}
	return recursivelyCopy(srcPath, dstPath)
}

func copyFile(srcPath, dstPath string) error {
	data, err := nodejs_dependens.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return os.WriteFile(dstPath, data, 0755)
}

func checkLibAvaliable() bool {
	if _, err := os.Stat(path.Join(LIB_PATH, LIB_NAME)); err != nil {
		return false
	}

	return true
}
