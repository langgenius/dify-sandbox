package python

import (
	_ "embed"
	"os"
	"os/exec"
	"path"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

//go:embed env.sh
var env_script string

func PreparePythonDependenciesEnv() error {
	config := static.GetDifySandboxGlobalConfigurations()

	runner := runner.TempDirRunner{}
	err := runner.WithTempDir("/", []string{}, func(root_path string) error {
		err := os.WriteFile(path.Join(root_path, "env.sh"), []byte(env_script), 0755)
		if err != nil {
			return err
		}

		for _, lib_path := range config.PythonLibPaths {
			// check if the lib path is available
			if _, err := os.Stat(lib_path); err != nil {
				log.Warn("python lib path %s is not available", lib_path)
				continue
			}
			exec_cmd := exec.Command(
				"bash",
				path.Join(root_path, "env.sh"),
				lib_path,
				LIB_PATH,
			)
			exec_cmd.Stderr = os.Stderr

			if err := exec_cmd.Run(); err != nil {
				return err
			}
		}

		os.RemoveAll(root_path)
		os.Remove(root_path)
		return nil
	})

	return err
}
