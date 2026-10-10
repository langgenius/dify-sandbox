//go:build linux

package static

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/types"
)

// Config tests run only in the disposable Linux CI fixture, never on the host.
func restrictedConfig() types.DifySandboxGlobalConfigurations {
	var c types.DifySandboxGlobalConfigurations
	c.Mode = "restricted"
	c.App.Port = 8194
	c.MaxWorkers = 1
	c.MaxRequests = 1
	c.WorkerTimeout = 5
	c.PythonPath = "/opt/python/bin/python3"
	c.NodejsPath = "/usr/local/bin/node"
	return c
}
func cleanConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{"SANDBOX_MODE", "DEBUG", "MAX_WORKERS", "MAX_REQUESTS", "SANDBOX_PORT", "WORKER_TIMEOUT", "API_KEY", "PYTHON_PATH", "PYTHON_LIB_PATH", "PIP_MIRROR_URL", "PYTHON_DEPS_UPDATE_INTERVAL", "NODEJS_PATH", "ENABLE_NETWORK", "ENABLE_PRELOAD", "ALLOWED_SYSCALLS", "SOCKS5_PROXY", "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY", "socks5_proxy", "https_proxy", "http_proxy", "all_proxy", "no_proxy"} {
		t.Setenv(key, "")
	}
}
func configPath(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

const restrictedYAML = "mode: restricted\napp:\n  port: 8194\nmax_workers: 1\nmax_requests: 1\nworker_timeout: 5\npython_path: /opt/python/bin/python3\nnodejs_path: /usr/local/bin/node\n"

func TestRestrictedConfiguration(t *testing.T) {
	cleanConfigEnvironment(t)
	if err := ValidateRestrictedConfiguration(restrictedConfig()); err != nil {
		t.Fatal(err)
	}
	if err := InitConfig("../../conf/restricted.yaml"); err != nil {
		t.Fatal(err)
	}
	c := GetDifySandboxGlobalConfigurations()
	if !c.RestrictedMode || c.Mode != "restricted" || c.PythonDepsUpdateInterval != "" || len(c.PythonLibPaths) != 0 {
		t.Fatalf("restricted defaults changed: %+v", c)
	}
}
func TestRestrictedConfigurationRejectsUnsafeSettings(t *testing.T) {
	for name, mutate := range map[string]func(*types.DifySandboxGlobalConfigurations){
		"unknown_mode":      func(c *types.DifySandboxGlobalConfigurations) { c.Mode = "unknown" },
		"workers":           func(c *types.DifySandboxGlobalConfigurations) { c.MaxWorkers = 2 },
		"requests":          func(c *types.DifySandboxGlobalConfigurations) { c.MaxRequests = 0 },
		"timeout":           func(c *types.DifySandboxGlobalConfigurations) { c.WorkerTimeout = 0 },
		"port":              func(c *types.DifySandboxGlobalConfigurations) { c.App.Port = 65536 },
		"debug":             func(c *types.DifySandboxGlobalConfigurations) { c.App.Debug = true },
		"network":           func(c *types.DifySandboxGlobalConfigurations) { c.EnableNetwork = true },
		"preload":           func(c *types.DifySandboxGlobalConfigurations) { c.EnablePreload = true },
		"syscalls":          func(c *types.DifySandboxGlobalConfigurations) { c.AllowedSyscalls = []int{0} },
		"http_proxy":        func(c *types.DifySandboxGlobalConfigurations) { c.Proxy.Http = "secret-proxy" },
		"https_proxy":       func(c *types.DifySandboxGlobalConfigurations) { c.Proxy.Https = "secret-proxy" },
		"socks_proxy":       func(c *types.DifySandboxGlobalConfigurations) { c.Proxy.Socks5 = "secret-proxy" },
		"dependency_timer":  func(c *types.DifySandboxGlobalConfigurations) { c.PythonDepsUpdateInterval = "30m" },
		"dependency_mirror": func(c *types.DifySandboxGlobalConfigurations) { c.PythonPipMirrorURL = "secret-mirror" },
		"dependency_paths":  func(c *types.DifySandboxGlobalConfigurations) { c.PythonLibPaths = []string{"/tmp/writable"} },
		"python_path":       func(c *types.DifySandboxGlobalConfigurations) { c.PythonPath = "/tmp/python" },
		"node_path":         func(c *types.DifySandboxGlobalConfigurations) { c.NodejsPath = "/tmp/node" },
	} {
		t.Run(name, func(t *testing.T) {
			c := restrictedConfig()
			mutate(&c)
			err := ValidateRestrictedConfiguration(c)
			if err == nil {
				t.Fatal("accepted unsafe config")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("leaked config value")
			}
		})
	}
}
func TestRestrictedConfigurationRejectsOverrides(t *testing.T) {
	for _, row := range []struct{ key, value string }{
		{"SANDBOX_MODE", "secret-unknown-mode"}, {"MAX_WORKERS", "secret-invalid"}, {"MAX_REQUESTS", "secret-invalid"}, {"SANDBOX_PORT", "secret-invalid"}, {"WORKER_TIMEOUT", "secret-invalid"},
		{"DEBUG", "secret-invalid"}, {"ENABLE_NETWORK", "secret-invalid"}, {"ENABLE_PRELOAD", "secret-invalid"}, {"ENABLE_NETWORK", "true"}, {"ENABLE_PRELOAD", "true"}, {"ALLOWED_SYSCALLS", "0"}, {"ALLOWED_SYSCALLS", "secret-invalid"},
		{"HTTP_PROXY", "secret-proxy"}, {"HTTPS_PROXY", "secret-proxy"}, {"SOCKS5_PROXY", "secret-proxy"}, {"ALL_PROXY", "secret-proxy"}, {"NO_PROXY", "secret-proxy"}, {"http_proxy", "secret-proxy"}, {"https_proxy", "secret-proxy"}, {"socks5_proxy", "secret-proxy"}, {"all_proxy", "secret-proxy"}, {"no_proxy", "secret-proxy"},
		{"PYTHON_DEPS_UPDATE_INTERVAL", "30m"}, {"PIP_MIRROR_URL", "secret-mirror"}, {"PYTHON_LIB_PATH", "/tmp/writable"}, {"PYTHON_PATH", "/tmp/python"}, {"NODEJS_PATH", "/tmp/node"},
	} {
		t.Run(row.key+row.value, func(t *testing.T) {
			cleanConfigEnvironment(t)
			t.Setenv(row.key, row.value)
			err := InitConfig(configPath(t, restrictedYAML))
			if err == nil {
				t.Fatal("accepted override")
			}
			if strings.Contains(err.Error(), row.value) {
				t.Fatal("leaked override")
			}
		})
	}
	for _, extra := range []string{"unknown_setting: true\n", "restricted_mode: true\n", "enable_network: true\n", "enable_preload: true\n", "allowed_syscalls: [0]\n", "python_deps_update_interval: 30m\n", "---\nmode: ordinary\n"} {
		t.Run(extra, func(t *testing.T) {
			cleanConfigEnvironment(t)
			if err := InitConfig(configPath(t, restrictedYAML+extra)); err == nil {
				t.Fatal("accepted invalid YAML")
			}
		})
	}
	// Replace the existing scalar: adding a second key would test duplicate
	// rejection while leaving fractional-to-integer coercion unexercised.
	for _, field := range []struct{ name, value string }{
		{"max_workers", "1"}, {"max_requests", "1"}, {"worker_timeout", "5"}, {"port", "8194"},
	} {
		for _, value := range []struct{ scalar, diagnostic string }{
			{field.value + ".9", "invalid_restricted_numeric_setting"},
			{"synthetic_invalid_number", "invalid_restricted_configuration"},
		} {
			t.Run(field.name+"/"+value.scalar, func(t *testing.T) {
				cleanConfigEnvironment(t)
				content := strings.Replace(restrictedYAML, field.name+": "+field.value+"\n", field.name+": "+value.scalar+"\n", 1)
				err := InitConfig(configPath(t, content))
				if err == nil || err.Error() != value.diagnostic {
					t.Fatalf("expected fixed %s diagnostic, got %v", value.diagnostic, err)
				}
			})
		}
	}
	for name, content := range map[string]string{
		"fractional_alias":      strings.Replace(strings.Replace(restrictedYAML, "max_workers: 1\n", "max_workers: &fractional 1.9\n", 1), "max_requests: 1\n", "max_requests: *fractional\n", 1),
		"fractional_root_merge": strings.Replace(restrictedYAML, "max_workers: 1\n", "<<: {max_workers: 1.9}\n", 1),
		"fractional_app_merge":  strings.Replace(restrictedYAML, "  port: 8194\n", "  <<: {port: 8194.9}\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			err := InitConfig(configPath(t, content))
			if err == nil || err.Error() != "invalid_restricted_numeric_setting" {
				t.Fatalf("expected fixed numeric diagnostic, got %v", err)
			}
		})
	}
	const marker = "synthetic_secret_duplicate_key"
	for name, content := range map[string]string{
		"duplicate_after_mode":  restrictedYAML + marker + ": first\n" + marker + ": second\n",
		"duplicate_before_mode": marker + ": first\n" + marker + ": second\n" + restrictedYAML,
	} {
		t.Run(name, func(t *testing.T) {
			cleanConfigEnvironment(t)
			err := InitConfig(configPath(t, content))
			if err == nil || err.Error() != "invalid_restricted_configuration" {
				t.Fatalf("expected fixed parse diagnostic, got %v", err)
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatal("leaked synthetic key marker")
			}
		})
	}
	// A safe env value must not hide an unsafe file value.
	cleanConfigEnvironment(t)
	t.Setenv("ENABLE_NETWORK", "false")
	if err := InitConfig(configPath(t, restrictedYAML+"enable_network: true\n")); err == nil {
		t.Fatal("silently erased unsafe file setting")
	}
}
func TestOrdinaryConfigurationDefaultsPreserved(t *testing.T) {
	cleanConfigEnvironment(t)
	if err := InitConfig(configPath(t, "app:\n  port: 8194\nenable_network: true\n")); err != nil {
		t.Fatal(err)
	}
	c := GetDifySandboxGlobalConfigurations()
	if c.Mode != "ordinary" || c.RestrictedMode || !c.EnableNetwork || c.PythonPath != "/opt/python/bin/python3" || c.NodejsPath != "/usr/local/bin/node" || c.PythonDepsUpdateInterval != "30m" || len(c.PythonLibPaths) == 0 {
		t.Fatalf("ordinary defaults changed: %+v", c)
	}
	// Ordinary YAML keeps its existing fractional decoding and raw duplicate
	// diagnostic behavior; strict node validation is restricted-only.
	if err := InitConfig(configPath(t, "mode: ordinary\nmax_workers: 1.9\n")); err != nil {
		t.Fatal(err)
	}
	if c := GetDifySandboxGlobalConfigurations(); c.MaxWorkers != 1 {
		t.Fatalf("ordinary numeric decoding changed: %d", c.MaxWorkers)
	}
	const marker = "synthetic_secret_duplicate_key"
	err := InitConfig(configPath(t, "mode: ordinary\n"+marker+": first\n"+marker+": second\n"))
	if err == nil || !strings.Contains(err.Error(), marker) {
		t.Fatalf("ordinary duplicate diagnostic changed: %v", err)
	}
	t.Setenv("MAX_WORKERS", "secret-invalid")
	t.Setenv("ENABLE_NETWORK", "secret-invalid")
	t.Setenv("ENABLE_PRELOAD", "secret-invalid")
	t.Setenv("DEBUG", "secret-invalid")
	t.Setenv("PYTHON_DEPS_UPDATE_INTERVAL", "17m")
	t.Setenv("PYTHON_LIB_PATH", "/tmp/a,/tmp/b")
	if err := InitConfig(configPath(t, "app:\n  debug: true\nenable_network: true\nenable_preload: true\n")); err != nil {
		t.Fatal(err)
	}
	c = GetDifySandboxGlobalConfigurations()
	if c.MaxWorkers != 0 || c.EnableNetwork || c.EnablePreload || !c.App.Debug || c.PythonDepsUpdateInterval != "17m" || len(c.PythonLibPaths) != 2 {
		t.Fatalf("ordinary env semantics changed: %+v", c)
	}
}
