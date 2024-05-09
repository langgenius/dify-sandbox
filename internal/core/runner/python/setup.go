package python

import (
	_ "embed"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"

	"github.com/langgenius/dify-sandbox/internal/core/runner"
	python_dependencies "github.com/langgenius/dify-sandbox/internal/core/runner/python/dependencies"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

//go:embed python.so
var python_lib []byte

func init() {
	log.Info("initializing python runner environment...")
	// remove /tmp/sandbox-python
	os.RemoveAll("/tmp/sandbox-python")
	os.Remove("/tmp/sandbox-python")

	err := os.MkdirAll("/tmp/sandbox-python", 0755)
	if err != nil {
		log.Panic("failed to create /tmp/sandbox-python")
	}
	err = os.WriteFile("/tmp/sandbox-python/python.so", python_lib, 0755)
	if err != nil {
		log.Panic("failed to write /tmp/sandbox-python/python.so")
	}
	log.Info("python runner environment initialized")
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
	return runner.WithTempDir([]string{}, func(root_path string) error {
		defer os.Remove(root_path)
		defer os.RemoveAll(root_path)
		// create a requirements file
		err := os.WriteFile(path.Join(root_path, "requirements.txt"), []byte(requirements), 0644)
		if err != nil {
			log.Error("failed to create requirements.txt")
			return nil
		}

		// install dependencies
		cmd := exec.Command("pip3", "install", "-r", "requirements.txt")

		reader, err := cmd.StdoutPipe()
		if err != nil {
			log.Panic("failed to get stdout pipe of pip3")
		}
		defer reader.Close()

		err = cmd.Start()
		if err != nil {
			log.Error("failed to start pip3")
			return nil
		}

		for {
			buf := make([]byte, 1024)
			n, err := reader.Read(buf)
			if err != nil {
				break
			}
			log.Info(string(buf[:n]))
		}

		status := cmd.Wait()

		if status != nil {
			log.Error("failed to install dependencies")
			return nil
		}

		// split the requirements
		requirements = strings.ReplaceAll(requirements, "\r\n", "\n")
		requirements = strings.ReplaceAll(requirements, "\r", "\n")
		lines := strings.Split(requirements, "\n")
		for _, line := range lines {
			package_name, version := ExtractOnelineDepency(line)
			if package_name == "" {
				continue
			}

			python_dependencies.SetupDependency(package_name, version, "")
			log.Info("Python dependency installed: %s %s", package_name, version)
		}

		return nil
	})
}

func ListDependencies() []types.Dependency {
	return python_dependencies.ListDependencies()
}
