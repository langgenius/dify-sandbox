package python

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"

	"github.com/langgenius/dify-sandbox/internal/static"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
	python_dependencies "github.com/langgenius/dify-sandbox/internal/core/runner/python/dependencies"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

//go:embed python.so
var python_lib []byte

const (
	LIB_PATH = "/var/sandbox/sandbox-python"
	LIB_NAME = "python.so"
)

func init() {
	releaseLibBinary(true)
}

func releaseLibBinary(force_remove_old_lib bool) {
	log.Info("initializing python runner environment...")
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
			err = os.WriteFile(path.Join(LIB_PATH, LIB_NAME), python_lib, 0755)
			if err != nil {
				log.Panic(fmt.Sprintf("failed to write %s", path.Join(LIB_PATH, LIB_NAME)))
			}
		}
	} else {
		err = os.MkdirAll(LIB_PATH, 0755)
		if err != nil {
			log.Panic(fmt.Sprintf("failed to create %s", LIB_PATH))
		}
		err = os.WriteFile(path.Join(LIB_PATH, LIB_NAME), python_lib, 0755)
		if err != nil {
			log.Panic(fmt.Sprintf("failed to write %s", path.Join(LIB_PATH, LIB_NAME)))
		}
		log.Info("python runner environment initialized")
	}
}

func checkLibAvaliable() bool {
	if _, err := os.Stat(path.Join(LIB_PATH, LIB_NAME)); err != nil {
		return false
	}

	return true
}

func ExtractOnelineDepency(dependency string) (string, string) {
	delimiters := []string{"==", ">=", "<=", "~="}
	for _, delimiter := range delimiters {
		if strings.Contains(dependency, delimiter) {
			parts := strings.Split(dependency, delimiter)
			if len(parts) >= 2 {
				return parts[0], parts[1]
			} else if len(parts) == 1 {
				return parts[0], ""
			} else if len(parts) == 0 {
				return "", ""
			}
		}
	}

	preg := regexp.MustCompile(`([a-zA-Z0-9_-]+)`)
	if preg.MatchString(dependency) {
		return dependency, ""
	}

	return "", ""
}

func InstallDependencies(requirements string) error {
	if requirements == "" {
		return nil
	}

	runner := runner.TempDirRunner{}
	return runner.WithTempDir("/", []string{}, func(root_path string) error {
		defer os.RemoveAll(root_path)
		// create a requirements file
		err := os.WriteFile(path.Join(root_path, "requirements.txt"), []byte(requirements), 0644)
		if err != nil {
			log.Error("failed to create requirements.txt")
			return nil
		}

		// install dependencies
		pipMirrorURL := static.GetDifySandboxGlobalConfigurations().PythonPipMirrorURL

		// Create the base command
		args := []string{"install", "-r", "requirements.txt"}
		if pipMirrorURL != "" {
			// If a mirror URL is provided, include it in the command arguments
			args = append(args, "-i", pipMirrorURL)
		}
		cmd := exec.Command("pip3", args...)
		reader, err := cmd.StdoutPipe()
		if err != nil {
			log.Error("failed to get stdout pipe of pip3")
			return err
		}
		defer reader.Close()

		err = cmd.Start()
		if err != nil {
			log.Error("failed to start pip3")
			return err
		}

		for {
			buf := make([]byte, 1024)
			n, err := reader.Read(buf)
			if err != nil {
				break
			}
			log.Info(string(buf[:n]))
		}

		err = cmd.Wait()

		if err != nil {
			log.Error("failed to wait for the command to complete")
			return err
		}

		// split the requirements
		requirements = strings.ReplaceAll(requirements, "\r\n", "\n")
		requirements = strings.ReplaceAll(requirements, "\r", "\n")
		lines := strings.Split(requirements, "\n")
		for _, line := range lines {
			packageName, version := ExtractOnelineDepency(line)
			if packageName == "" {
				continue
			}

			python_dependencies.SetupDependency(packageName, version)
			log.Info("Python dependency installed: %s %s", packageName, version)
		}

		return nil
	})
}

func ListDependencies() []types.Dependency {
	return python_dependencies.ListDependencies()
}

func RefreshDependencies() []types.Dependency {
	log.Info("updating python dependencies...")
	dependencies := static.GetRunnerDependencies()
	err := InstallDependencies(dependencies.PythonRequirements)
	if err != nil {
		log.Error("failed to install python dependencies: %v", err)
		return nil
	}
	log.Info("python dependencies updated")
	return python_dependencies.ListDependencies()
}
