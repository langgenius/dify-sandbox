package static

import (
	"errors"
	"io"
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

	// read config file
	configFile, err := os.Open(path)
	if err != nil {
		return err
	}

	defer configFile.Close()

	// parse config file
	decoder := yaml.NewDecoder(configFile)
	err = decoder.Decode(&difySandboxGlobalConfigurations)
	if err != nil {
		// A duplicate root key can fail before typed decoding populates Mode.
		// Probe parsed nodes independently for the fixed restricted diagnostic.
		if difySandboxGlobalConfigurations.Mode == "restricted" || os.Getenv("SANDBOX_MODE") == "restricted" || restrictedConfigurationIntent(configFile) {
			return errors.New("invalid_restricted_configuration")
		}
		return err
	}

	mode := difySandboxGlobalConfigurations.Mode
	if mode != "" && mode != "ordinary" && mode != "restricted" {
		return errors.New("invalid_sandbox_mode")
	}
	if value := os.Getenv("SANDBOX_MODE"); value != "" {
		mode = value
	}
	if mode == "" {
		mode = "ordinary"
	}
	if mode != "ordinary" && mode != "restricted" {
		return errors.New("invalid_sandbox_mode")
	}
	difySandboxGlobalConfigurations.Mode = mode
	if mode == "restricted" {
		// Re-decode strictly before any overrides can erase unsafe file settings.
		if _, err := configFile.Seek(0, io.SeekStart); err != nil {
			return errors.New("invalid_restricted_configuration")
		}
		var document yaml.Node
		if err := yaml.NewDecoder(configFile).Decode(&document); err != nil {
			return errors.New("invalid_restricted_configuration")
		}
		if err := validateRestrictedIntegerNodes(&document); err != nil {
			return err
		}
		if _, err := configFile.Seek(0, io.SeekStart); err != nil {
			return errors.New("invalid_restricted_configuration")
		}
		var config types.DifySandboxGlobalConfigurations
		strict := yaml.NewDecoder(configFile)
		strict.KnownFields(true)
		if err := strict.Decode(&config); err != nil {
			return errors.New("invalid_restricted_configuration")
		}
		var extra interface{}
		if err := strict.Decode(&extra); err != io.EOF {
			return errors.New("invalid_restricted_configuration")
		}
		config.Mode = mode
		if err := ValidateRestrictedConfiguration(config); err != nil {
			return err
		}
		if err := applyRestrictedEnvironment(&config); err != nil {
			return err
		}
		if err := ValidateRestrictedConfiguration(config); err != nil {
			return err
		}
		config.RestrictedMode = true
		difySandboxGlobalConfigurations = config
		return nil
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

	python_lib_path := os.Getenv("PYTHON_LIB_PATH")
	if python_lib_path != "" {
		difySandboxGlobalConfigurations.PythonLibPaths = strings.Split(python_lib_path, ",")
	}

	if len(difySandboxGlobalConfigurations.PythonLibPaths) == 0 {
		difySandboxGlobalConfigurations.PythonLibPaths = DEFAULT_PYTHON_LIB_REQUIREMENTS
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

// ValidateRestrictedConfiguration validates server settings only. It does not
// validate assets, establish readiness, or authorize a capability receipt.
func ValidateRestrictedConfiguration(config types.DifySandboxGlobalConfigurations) error {
	if config.Mode != "" && config.Mode != "ordinary" && config.Mode != "restricted" {
		return errors.New("invalid_sandbox_mode")
	}
	if config.Mode != "restricted" {
		return nil
	}
	if config.App.Port < 1 || config.App.Port > 65535 || config.MaxWorkers != 1 || config.MaxRequests != 1 || config.WorkerTimeout != 5 {
		return errors.New("invalid_restricted_numeric_setting")
	}
	if config.App.Debug || config.EnableNetwork || config.EnablePreload {
		return errors.New("unsafe_restricted_setting")
	}
	if len(config.AllowedSyscalls) != 0 {
		return errors.New("restricted_syscall_override")
	}
	if config.Proxy.Socks5 != "" || config.Proxy.Http != "" || config.Proxy.Https != "" {
		return errors.New("restricted_proxy_setting")
	}
	if len(config.PythonLibPaths) != 0 || config.PythonPipMirrorURL != "" || config.PythonDepsUpdateInterval != "" {
		return errors.New("restricted_dependency_mutation")
	}
	if config.PythonPath != "/opt/python/bin/python3" || config.NodejsPath != "/usr/local/bin/node" {
		return errors.New("restricted_interpreter_override")
	}
	return nil
}

func applyRestrictedEnvironment(config *types.DifySandboxGlobalConfigurations) error {
	for _, field := range []struct {
		key    string
		target *int
	}{{"MAX_WORKERS", &config.MaxWorkers}, {"MAX_REQUESTS", &config.MaxRequests}, {"SANDBOX_PORT", &config.App.Port}, {"WORKER_TIMEOUT", &config.WorkerTimeout}} {
		if value := os.Getenv(field.key); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return errors.New("invalid_restricted_numeric_setting")
			}
			*field.target = parsed
		}
	}
	for _, field := range []struct {
		key    string
		target *bool
	}{{"DEBUG", &config.App.Debug}, {"ENABLE_NETWORK", &config.EnableNetwork}, {"ENABLE_PRELOAD", &config.EnablePreload}} {
		if value := os.Getenv(field.key); value != "" {
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return errors.New("invalid_restricted_boolean_setting")
			}
			*field.target = parsed
		}
	}
	for _, key := range []string{"ALLOWED_SYSCALLS", "SOCKS5_PROXY", "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY", "socks5_proxy", "https_proxy", "http_proxy", "all_proxy", "no_proxy", "PYTHON_LIB_PATH", "PIP_MIRROR_URL", "PYTHON_DEPS_UPDATE_INTERVAL"} {
		if os.Getenv(key) != "" {
			return errors.New("unsafe_restricted_environment")
		}
	}
	for _, field := range []struct {
		key    string
		target *string
	}{{"API_KEY", &config.App.Key}, {"PYTHON_PATH", &config.PythonPath}, {"NODEJS_PATH", &config.NodejsPath}} {
		if value := os.Getenv(field.key); value != "" {
			*field.target = value
		}
	}
	return nil
}

// restrictedConfigurationIntent is diagnostic classification only. It is
// independent of failed typed decoding and never supplies the configured mode.
func restrictedConfigurationIntent(file *os.File) bool {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false
	}
	var document yaml.Node
	if err := yaml.NewDecoder(file).Decode(&document); err != nil {
		return false
	}
	mode, _ := configurationNodeMode(&document, make(map[*yaml.Node]bool))
	return mode == "restricted"
}

func configurationNodeMode(node *yaml.Node, seen map[*yaml.Node]bool) (string, bool) {
	if node == nil || seen[node] {
		return "", false
	}
	seen[node] = true
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			if mode, found := configurationNodeMode(child, seen); found {
				return mode, true
			}
		}
	case yaml.AliasNode:
		return configurationNodeMode(node.Alias, seen)
	case yaml.MappingNode:
		// A direct mode overrides merge defaults. Duplicate direct modes cannot
		// decode successfully; any restricted value keeps diagnostics fixed.
		mode, found := "", false
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value != "mode" {
				continue
			}
			found = true
			value := configurationScalarNode(node.Content[i+1])
			if value != nil && value.Kind == yaml.ScalarNode {
				mode = value.Value
				if mode == "restricted" {
					return mode, true
				}
			}
		}
		if found {
			return mode, true
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Tag == "!!merge" {
				if mode, found := configurationNodeMode(node.Content[i+1], seen); found {
					return mode, true
				}
			}
		}
	}
	return "", false
}

func configurationScalarNode(node *yaml.Node) *yaml.Node {
	seen := make(map[*yaml.Node]bool)
	for node != nil && node.Kind == yaml.AliasNode {
		if seen[node] {
			return nil
		}
		seen[node] = true
		node = node.Alias
	}
	return node
}

// walkConfigurationMappings follows document/alias/merge wrappers only. It
// visits the current mapping without descending into arbitrary nested settings.
func walkConfigurationMappings(node *yaml.Node, seen map[*yaml.Node]bool, visit func(*yaml.Node)) {
	if node == nil || seen[node] {
		return
	}
	seen[node] = true
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			walkConfigurationMappings(child, seen, visit)
		}
	case yaml.AliasNode:
		walkConfigurationMappings(node.Alias, seen, visit)
	case yaml.MappingNode:
		visit(node)
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Tag == "!!merge" {
				walkConfigurationMappings(node.Content[i+1], seen, visit)
			}
		}
	}
}

// yaml.v3 can convert !!float values to integer fields. Check the four current
// restricted integer settings as actual integers before strict typed decoding.
func validateRestrictedIntegerNodes(document *yaml.Node) error {
	valid := true
	checkInteger := func(node *yaml.Node) {
		node = configurationScalarNode(node)
		if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
			valid = false
		}
	}
	walkConfigurationMappings(document, make(map[*yaml.Node]bool), func(mapping *yaml.Node) {
		for i := 0; i+1 < len(mapping.Content); i += 2 {
			switch mapping.Content[i].Value {
			case "max_workers", "max_requests", "worker_timeout":
				checkInteger(mapping.Content[i+1])
			case "app":
				walkConfigurationMappings(mapping.Content[i+1], make(map[*yaml.Node]bool), func(app *yaml.Node) {
					for j := 0; j+1 < len(app.Content); j += 2 {
						if app.Content[j].Value == "port" {
							checkInteger(app.Content[j+1])
						}
					}
				})
			}
		}
	})
	if !valid {
		return errors.New("invalid_restricted_numeric_setting")
	}
	return nil
}
