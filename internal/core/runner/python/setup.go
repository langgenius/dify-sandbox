package python

import (
	_ "embed"
	"fmt"
	"io"
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

func releaseLibBinary(forceRemoveOldLib bool) {
	log.Info("initializing python runner environment...")

	libExists := pythonLibExists()
	if libExists && !forceRemoveOldLib {
		log.Info("python runner environment already initialized")
		return
	}

	if libExists && forceRemoveOldLib {
		removePythonLib()
	}

	ensurePythonLibDir()
	writePythonLib()
	log.Info("python runner environment initialized")
}

func pythonLibExists() bool {
	_, err := os.Stat(path.Join(LIB_PATH, LIB_NAME))
	return err == nil
}

func removePythonLib() {
	if err := os.Remove(path.Join(LIB_PATH, LIB_NAME)); err != nil {
		log.Panic(fmt.Sprintf("failed to remove %s", path.Join(LIB_PATH, LIB_NAME)))
	}
}

func ensurePythonLibDir() {
	if err := os.MkdirAll(LIB_PATH, 0755); err != nil {
		log.Panic(fmt.Sprintf("failed to create %s", LIB_PATH))
	}
}

func writePythonLib() {
	if err := os.WriteFile(path.Join(LIB_PATH, LIB_NAME), python_lib, 0755); err != nil {
		log.Panic(fmt.Sprintf("failed to write %s", path.Join(LIB_PATH, LIB_NAME)))
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
	return runner.WithTempDir("/", []string{}, func(rootPath string) error {
		defer os.RemoveAll(rootPath)

		if err := writeRequirementsFile(rootPath, requirements); err != nil {
			log.Error("failed to create requirements.txt")
			return nil
		}

		if err := runPipInstall(rootPath); err != nil {
			return err
		}

		installLocalDependencies(requirements)

		return nil
	})
}

func writeRequirementsFile(rootPath string, requirements string) error {
	return os.WriteFile(path.Join(rootPath, "requirements.txt"), []byte(requirements), 0644)
}

func runPipInstall(rootPath string) error {
	cmd, reader, err := setupPipCommand(rootPath)
	if err != nil {
		log.Error("failed to get stdout pipe of pip3")
		return err
	}
	defer reader.Close()

	if err := cmd.Start(); err != nil {
		log.Error("failed to start pip3")
		return err
	}

	streamCommandOutput(reader)

	if err := cmd.Wait(); err != nil {
		log.Error("failed to wait for the command to complete")
		return err
	}

	return nil
}

func setupPipCommand(rootPath string) (*exec.Cmd, io.ReadCloser, error) {
	args := buildPipArgs()
	cmd := exec.Command("pip3", args...)
	cmd.Dir = rootPath

	reader, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	return cmd, reader, nil
}

func buildPipArgs() []string {
	args := []string{"install", "-r", "requirements.txt"}
	pipMirrorURL := static.GetDifySandboxGlobalConfigurations().PythonPipMirrorURL
	if pipMirrorURL != "" {
		args = append(args, "-i", pipMirrorURL)
	}
	return args
}

func streamCommandOutput(reader io.ReadCloser) {
	for {
		buf := make([]byte, 1024)
		n, err := reader.Read(buf)
		if err != nil {
			break
		}
		log.Info(string(buf[:n]))
	}
}

func installLocalDependencies(requirements string) {
	for _, line := range normalizeRequirementLines(requirements) {
		packageName, version := ExtractOnelineDepency(line)
		if packageName == "" {
			continue
		}

		python_dependencies.SetupDependency(packageName, version)
		log.Info("Python dependency installed: %s %s", packageName, version)
	}
}

func normalizeRequirementLines(requirements string) []string {
	normalized := strings.ReplaceAll(requirements, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.Split(normalized, "\n")
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
