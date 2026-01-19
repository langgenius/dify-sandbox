package static

import (
	"os"
	"strconv"
	"strings"

	"github.com/langgenius/dify-sandbox/internal/types"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
	"gopkg.in/yaml.v3"
)

var difySandboxGlobalConfigurations types.DifySandboxGlobalConfigurations

func InitConfig(path string) error {
	difySandboxGlobalConfigurations = types.DifySandboxGlobalConfigurations{}

	if err := loadConfigFromFile(path); err != nil {
		return err
	}

	overrideAppConfig()
	overrideWorkerSettings()
	overridePythonConfig()
	overrideNodeConfig()

	if err := overrideSyscalls(); err != nil {
		return err
	}

	handleNetworkConfig()
	return nil
}

func loadConfigFromFile(path string) error {
	configFile, err := os.Open(path)
	if err != nil {
		return err
	}

	defer configFile.Close()

	decoder := yaml.NewDecoder(configFile)
	return decoder.Decode(&difySandboxGlobalConfigurations)
}

func overrideAppConfig() {
	if debugStr := os.Getenv("DEBUG"); debugStr != "" {
		if debug, err := strconv.ParseBool(debugStr); err == nil {
			difySandboxGlobalConfigurations.App.Debug = debug
		}
	}

	if port := readIntEnv("SANDBOX_PORT"); port != nil {
		difySandboxGlobalConfigurations.App.Port = *port
	}

	if apiKey := os.Getenv("API_KEY"); apiKey != "" {
		difySandboxGlobalConfigurations.App.Key = apiKey
	}
}

func overrideWorkerSettings() {
	if maxWorkers := readIntEnv("MAX_WORKERS"); maxWorkers != nil {
		difySandboxGlobalConfigurations.MaxWorkers = *maxWorkers
	}

	if maxRequests := readIntEnv("MAX_REQUESTS"); maxRequests != nil {
		difySandboxGlobalConfigurations.MaxRequests = *maxRequests
	}

	if timeout := readIntEnv("WORKER_TIMEOUT"); timeout != nil {
		difySandboxGlobalConfigurations.WorkerTimeout = *timeout
	}

	if preload := os.Getenv("ENABLE_PRELOAD"); preload != "" {
		difySandboxGlobalConfigurations.EnablePreload, _ = strconv.ParseBool(preload)
	}
}

func overridePythonConfig() {
	if pythonPath := os.Getenv("PYTHON_PATH"); pythonPath != "" {
		difySandboxGlobalConfigurations.PythonPath = pythonPath
	}

	if difySandboxGlobalConfigurations.PythonPath == "" {
		difySandboxGlobalConfigurations.PythonPath = "/opt/python/bin/python3"
	}

	if pythonLibPath := os.Getenv("PYTHON_LIB_PATH"); pythonLibPath != "" {
		difySandboxGlobalConfigurations.PythonLibPaths = strings.Split(pythonLibPath, ",")
	}

	if len(difySandboxGlobalConfigurations.PythonLibPaths) == 0 {
		difySandboxGlobalConfigurations.PythonLibPaths = DEFAULT_PYTHON_LIB_REQUIREMENTS
	}

	if pipMirrorURL := os.Getenv("PIP_MIRROR_URL"); pipMirrorURL != "" {
		difySandboxGlobalConfigurations.PythonPipMirrorURL = pipMirrorURL
	}

	if depsInterval := os.Getenv("PYTHON_DEPS_UPDATE_INTERVAL"); depsInterval != "" {
		difySandboxGlobalConfigurations.PythonDepsUpdateInterval = depsInterval
	}

	if difySandboxGlobalConfigurations.PythonDepsUpdateInterval == "" {
		difySandboxGlobalConfigurations.PythonDepsUpdateInterval = "30m"
	}
}

func overrideNodeConfig() {
	if nodePath := os.Getenv("NODEJS_PATH"); nodePath != "" {
		difySandboxGlobalConfigurations.NodejsPath = nodePath
	}

	if difySandboxGlobalConfigurations.NodejsPath == "" {
		difySandboxGlobalConfigurations.NodejsPath = "/usr/local/bin/node"
	}
}

func overrideSyscalls() error {
	allowedSyscalls := os.Getenv("ALLOWED_SYSCALLS")
	if allowedSyscalls == "" {
		return nil
	}

	strs := strings.Split(allowedSyscalls, ",")
	ary := make([]int, len(strs))
	for i := range ary {
		value, err := strconv.Atoi(strs[i])
		if err != nil {
			return err
		}
		ary[i] = value
	}

	difySandboxGlobalConfigurations.AllowedSyscalls = ary
	return nil
}

func handleNetworkConfig() {
	overrideNetworkFlags()

	if !difySandboxGlobalConfigurations.EnableNetwork {
		return
	}

	log.Info("network has been enabled")
	overrideProxy("SOCKS5_PROXY", &difySandboxGlobalConfigurations.Proxy.Socks5, "socks5 proxy")
	overrideProxy("HTTPS_PROXY", &difySandboxGlobalConfigurations.Proxy.Https, "https proxy")
	overrideProxy("HTTP_PROXY", &difySandboxGlobalConfigurations.Proxy.Http, "http proxy")
}

func overrideNetworkFlags() {
	if enableNetwork := os.Getenv("ENABLE_NETWORK"); enableNetwork != "" {
		difySandboxGlobalConfigurations.EnableNetwork, _ = strconv.ParseBool(enableNetwork)
	}
}

func overrideProxy(env string, target *string, logLabel string) {
	if value := os.Getenv(env); value != "" {
		*target = value
	}

	if *target != "" {
		log.Info("using %s: %s", logLabel, *target)
	}
}

func readIntEnv(key string) *int {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}

	number, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return &number
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
