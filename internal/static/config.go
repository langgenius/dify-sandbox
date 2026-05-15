package static

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/langgenius/dify-sandbox/internal/types"
	"gopkg.in/yaml.v3"
)

var difySandboxGlobalConfigurations types.DifySandboxGlobalConfigurations

func InitConfig(path string) error {
	difySandboxGlobalConfigurations = types.DifySandboxGlobalConfigurations{}

	configContent, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var rawConfig map[string]any
	if err = yaml.Unmarshal(configContent, &rawConfig); err != nil {
		return err
	}

	err = yaml.Unmarshal(configContent, &difySandboxGlobalConfigurations)
	if err != nil {
		return err
	}

	if _, ok := rawConfig["python_lib_path"]; ok {
		slog.Warn("python_lib_path is deprecated and ignored; python library paths are discovered from python_path")
	}

	debug, err := strconv.ParseBool(os.Getenv("DEBUG"))
	if err == nil {
		difySandboxGlobalConfigurations.App.Debug = debug
	}

	max_workers := os.Getenv("MAX_WORKERS")
	if max_workers != "" {
		difySandboxGlobalConfigurations.MaxWorkers, _ = strconv.Atoi(max_workers)
	}

	max_requests := os.Getenv("MAX_REQUESTS")
	if max_requests != "" {
		difySandboxGlobalConfigurations.MaxRequests, _ = strconv.Atoi(max_requests)
	}

	port := os.Getenv("SANDBOX_PORT")
	if port != "" {
		difySandboxGlobalConfigurations.App.Port, _ = strconv.Atoi(port)
	}

	timeout := os.Getenv("WORKER_TIMEOUT")
	if timeout != "" {
		difySandboxGlobalConfigurations.WorkerTimeout, _ = strconv.Atoi(timeout)
	}

	api_key := os.Getenv("API_KEY")
	if api_key != "" {
		difySandboxGlobalConfigurations.App.Key = api_key
	}

	python_path := os.Getenv("PYTHON_PATH")
	if python_path != "" {
		difySandboxGlobalConfigurations.PythonPath = python_path
	}

	if difySandboxGlobalConfigurations.PythonPath == "" {
		difySandboxGlobalConfigurations.PythonPath = "/opt/python/bin/python3"
	}

	if os.Getenv("PYTHON_LIB_PATH") != "" {
		slog.Warn("PYTHON_LIB_PATH is deprecated and ignored; python library paths are discovered from python_path")
	}

	// PythonLibPaths is a derived runtime field, not a user-configurable one.
	// Resolve the interpreter first, then discover its stdlib/site paths here so
	// downstream sandbox preparation only copies interpreter-owned paths.
	resolvedPythonPath, err := resolvePythonPath(difySandboxGlobalConfigurations.PythonPath)
	if err != nil {
		return fmt.Errorf("resolve python_path %q: %w", difySandboxGlobalConfigurations.PythonPath, err)
	}
	difySandboxGlobalConfigurations.PythonPath = resolvedPythonPath

	difySandboxGlobalConfigurations.PythonLibPaths, err = discoverPythonLibPaths(difySandboxGlobalConfigurations.PythonPath)
	if err != nil {
		return fmt.Errorf("discover python library paths from %q: %w", difySandboxGlobalConfigurations.PythonPath, err)
	}

	python_pip_mirror_url := os.Getenv("PIP_MIRROR_URL")
	if python_pip_mirror_url != "" {
		difySandboxGlobalConfigurations.PythonPipMirrorURL = python_pip_mirror_url
	}

	python_deps_update_interval := os.Getenv("PYTHON_DEPS_UPDATE_INTERVAL")
	if python_deps_update_interval != "" {
		difySandboxGlobalConfigurations.PythonDepsUpdateInterval = python_deps_update_interval
	}

	// if not set "PythonDepsUpdateInterval", update python dependencies every 30 minutes to keep the sandbox up-to-date
	if difySandboxGlobalConfigurations.PythonDepsUpdateInterval == "" {
		difySandboxGlobalConfigurations.PythonDepsUpdateInterval = "30m"
	}

	nodejs_path := os.Getenv("NODEJS_PATH")
	if nodejs_path != "" {
		difySandboxGlobalConfigurations.NodejsPath = nodejs_path
	}

	if difySandboxGlobalConfigurations.NodejsPath == "" {
		difySandboxGlobalConfigurations.NodejsPath = "/usr/local/bin/node"
	}

	enable_network := os.Getenv("ENABLE_NETWORK")
	if enable_network != "" {
		difySandboxGlobalConfigurations.EnableNetwork, _ = strconv.ParseBool(enable_network)
	}

	enable_preload := os.Getenv("ENABLE_PRELOAD")
	if enable_preload != "" {
		difySandboxGlobalConfigurations.EnablePreload, _ = strconv.ParseBool(enable_preload)
	}

	allowed_syscalls := os.Getenv("ALLOWED_SYSCALLS")
	if allowed_syscalls != "" {
		strs := strings.Split(allowed_syscalls, ",")
		ary := make([]int, len(strs))
		for i := range ary {
			ary[i], err = strconv.Atoi(strs[i])
			if err != nil {
				return err
			}
		}
		difySandboxGlobalConfigurations.AllowedSyscalls = ary
	}

	if difySandboxGlobalConfigurations.EnableNetwork {
		slog.Info("network has been enabled")
		socks5_proxy := os.Getenv("SOCKS5_PROXY")
		if socks5_proxy != "" {
			difySandboxGlobalConfigurations.Proxy.Socks5 = socks5_proxy
		}

		if difySandboxGlobalConfigurations.Proxy.Socks5 != "" {
			slog.Info("using socks5 proxy", "proxy", difySandboxGlobalConfigurations.Proxy.Socks5)
		}

		https_proxy := os.Getenv("HTTPS_PROXY")
		if https_proxy != "" {
			difySandboxGlobalConfigurations.Proxy.Https = https_proxy
		}

		if difySandboxGlobalConfigurations.Proxy.Https != "" {
			slog.Info("using https proxy", "proxy", difySandboxGlobalConfigurations.Proxy.Https)
		}

		http_proxy := os.Getenv("HTTP_PROXY")
		if http_proxy != "" {
			difySandboxGlobalConfigurations.Proxy.Http = http_proxy
		}

		if difySandboxGlobalConfigurations.Proxy.Http != "" {
			slog.Info("using http proxy", "proxy", difySandboxGlobalConfigurations.Proxy.Http)
		}
	}
	return nil
}

// avoid global modification, use value copy instead
func GetDifySandboxGlobalConfigurations() types.DifySandboxGlobalConfigurations {
	return difySandboxGlobalConfigurations
}

type RunnerDependencies struct {
	PythonRequirements string
}

var runnerDependencies RunnerDependencies

func GetRunnerDependencies() RunnerDependencies {
	return runnerDependencies
}

func SetupRunnerDependencies() error {
	file, err := os.ReadFile("dependencies/python-requirements.txt")
	if err != nil {
		if err == os.ErrNotExist {
			return nil
		}
		return err
	}

	runnerDependencies.PythonRequirements = string(file)

	return nil
}
