package types

type DifySandboxGlobalConfigurations struct {
	App struct {
		Port  int    `yaml:"port"`
		Debug bool   `yaml:"debug"`
		Key   string `yaml:"key"`
	} `yaml:"app"`
	MaxWorkers    int    `yaml:"max_workers"`
	MaxRequests   int    `yaml:"max_requests"`
	WorkerTimeout int    `yaml:"worker_timeout"`
	PythonPath    string `yaml:"python_path"`
	// PythonLibPaths is internal-only. InitConfig derives it from PythonPath at
	// startup, and legacy python_lib_path / PYTHON_LIB_PATH user inputs are
	// intentionally ignored.
	PythonLibPaths           []string `yaml:"-"`
	PythonPipMirrorURL       string   `yaml:"python_pip_mirror_url"`
	PythonDepsUpdateInterval string   `yaml:"python_deps_update_interval"`
	NodejsPath               string   `yaml:"nodejs_path"`
	EnableNetwork            bool     `yaml:"enable_network"`
	EnablePreload            bool     `yaml:"enable_preload"`
	AllowedSyscalls          []int    `yaml:"allowed_syscalls"`
	LogPath                  string   `yaml:"log_path"`
	Proxy                    struct {
		Socks5  string `yaml:"socks5"`
		Https   string `yaml:"https"`
		Http    string `yaml:"http"`
		NoProxy string `yaml:"no_proxy"`
	} `yaml:"proxy"`
	AllowedEnvVars []string `yaml:"allowed_env_vars"`

	// SecretInjection configures the credential-injection outbound proxy.
	// When enabled, real secrets are replaced with placeholders in the
	// agent's environment and resolved at the network boundary.  See
	// internal/core/proxy/ and issue #39278 for design rationale.
	SecretInjection struct {
		// Enabled toggles the proxy on/off.
		Enabled bool `yaml:"enabled"`
		// ProvidersPath is the path to providers.yaml (absolute or
		// relative to the config file directory).
		ProvidersPath string `yaml:"providers_path"`
		// SecretRoot is the directory containing secret value files
		// (e.g. /run/secrets/<session-id>/).  Must be outside the
		// agent's Landlock-restricted workspace.
		SecretRoot string `yaml:"secret_root"`
	} `yaml:"secret_injection"`
}
