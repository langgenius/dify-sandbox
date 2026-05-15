package static

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverPythonLibPathsFindsStdlibAndJSON(t *testing.T) {
	pythonPath := mustFindTestPython(t)

	paths, err := discoverPythonLibPaths(pythonPath)
	if err != nil {
		t.Fatalf("discoverPythonLibPaths returned error: %v", err)
	}

	if len(paths) == 0 {
		t.Fatal("expected discovered python library paths")
	}

	jsonRootFound := false
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			t.Fatalf("expected absolute path, got %q", path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected existing path %q: %v", path, err)
		}
		if strings.HasSuffix(path, ".zip") {
			jsonRootFound = true
		}
		if _, err := os.Stat(filepath.Join(path, "json")); err == nil {
			jsonRootFound = true
		}
		if _, err := os.Stat(filepath.Join(path, "json.py")); err == nil {
			jsonRootFound = true
		}
	}

	if !jsonRootFound {
		t.Fatal("expected discovered paths to include a json module parent path")
	}
}

func TestDiscoverPythonLibPathsIgnoresCurrentWorkingDirectoryShadowing(t *testing.T) {
	pythonPath := mustFindTestPython(t)
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "json.py"), []byte("raise RuntimeError('shadowed json should not load')\n"), 0o600); err != nil {
		t.Fatalf("write shadow json: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}

	paths, err := discoverPythonLibPaths(pythonPath)
	if err != nil {
		t.Fatalf("discoverPythonLibPaths returned error with cwd shadowing: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("expected discovered python library paths")
	}
}

func TestInitConfigIgnoresPythonLibPathInputs(t *testing.T) {
	pythonPath := mustFindTestPython(t)
	configPath := writeTempConfig(t, "app:\n  port: 8194\npython_path: "+pythonPath+"\npython_lib_path:\n  - /tmp/should-not-be-used-from-yaml\n")

	t.Setenv("PYTHON_PATH", "")
	t.Setenv("PYTHON_LIB_PATH", "/tmp/should-not-be-used-from-env")

	if err := InitConfig(configPath); err != nil {
		t.Fatalf("InitConfig returned error: %v", err)
	}

	config := GetDifySandboxGlobalConfigurations()
	for _, path := range config.PythonLibPaths {
		if strings.Contains(path, "should-not-be-used") {
			t.Fatalf("deprecated python_lib_path input affected final paths: %v", config.PythonLibPaths)
		}
	}

	if len(config.PythonLibPaths) == 0 {
		t.Fatal("expected discovered python library paths")
	}
	if !filepath.IsAbs(config.PythonPath) {
		t.Fatalf("expected resolved absolute python path, got %q", config.PythonPath)
	}
}

func TestInitConfigFailsForInvalidPythonPath(t *testing.T) {
	configPath := writeTempConfig(t, "app:\n  port: 8194\npython_path: /definitely/missing/python\n")
	t.Setenv("PYTHON_PATH", "")

	err := InitConfig(configPath)
	if err == nil {
		t.Fatal("expected InitConfig to fail for invalid python_path")
	}
	if !strings.Contains(err.Error(), "python_path") {
		t.Fatalf("expected error to mention python_path, got %v", err)
	}
}

func TestBuildPythonLibPathsFailsWithoutStdlibOrJSONRoot(t *testing.T) {
	t.Run("missing stdlib", func(t *testing.T) {
		_, err := buildPythonLibPaths(pythonLibDiscoveryResult{
			Stdlib:           "/path/does/not/exist",
			JSONOrigin:       os.Args[0],
			JSONRelativePath: "json/__init__.py",
			JSONRoot:         filepath.Dir(os.Args[0]),
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "standard library") {
			t.Fatalf("expected standard library validation error, got %v", err)
		}
	})

	t.Run("missing json", func(t *testing.T) {
		_, err := buildPythonLibPaths(pythonLibDiscoveryResult{
			Stdlib:           filepath.Dir(os.Args[0]),
			JSONOrigin:       "/path/does/not/exist/json/__init__.py",
			JSONRelativePath: "json/__init__.py",
			JSONRoot:         "/path/does/not/exist",
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "json module") {
			t.Fatalf("expected json validation error, got %v", err)
		}
	})

	t.Run("shadowed json outside stdlib", func(t *testing.T) {
		tempDir := t.TempDir()
		stdlibPath := filepath.Join(tempDir, "stdlib")
		shadowedPath := filepath.Join(tempDir, "site-packages")
		if err := os.MkdirAll(stdlibPath, 0o755); err != nil {
			t.Fatalf("mkdir stdlib: %v", err)
		}
		if err := os.MkdirAll(shadowedPath, 0o755); err != nil {
			t.Fatalf("mkdir shadowed path: %v", err)
		}

		_, err := buildPythonLibPaths(pythonLibDiscoveryResult{
			Stdlib:           stdlibPath,
			JSONOrigin:       filepath.Join(shadowedPath, "json", "__init__.py"),
			JSONRelativePath: filepath.Join("json", "__init__.py"),
			JSONRoot:         shadowedPath,
			Paths:            []string{stdlibPath, shadowedPath},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "standard library") {
			t.Fatalf("expected stdlib ownership error, got %v", err)
		}
	})

	t.Run("shadowed json under site-packages inside stdlib prefix", func(t *testing.T) {
		tempDir := t.TempDir()
		stdlibPath := filepath.Join(tempDir, "python3.12")
		shadowedPath := filepath.Join(stdlibPath, "site-packages")
		if err := os.MkdirAll(shadowedPath, 0o755); err != nil {
			t.Fatalf("mkdir shadowed path: %v", err)
		}

		_, err := buildPythonLibPaths(pythonLibDiscoveryResult{
			Stdlib:           stdlibPath,
			JSONOrigin:       filepath.Join(shadowedPath, "json", "__init__.py"),
			JSONRelativePath: filepath.Join("site-packages", "json", "__init__.py"),
			JSONRoot:         stdlibPath,
			Paths:            []string{stdlibPath, shadowedPath},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "canonical standard-library json location") {
			t.Fatalf("expected canonical stdlib json location error, got %v", err)
		}
	})
}

func TestBuildPythonLibPathsKeepsLogicalPythonAndSystemPaths(t *testing.T) {
	tempDir := t.TempDir()
	realStdlib := filepath.Join(tempDir, "real-stdlib")
	if err := os.MkdirAll(filepath.Join(realStdlib, "json"), 0o755); err != nil {
		t.Fatalf("mkdir real stdlib: %v", err)
	}
	jsonFile := filepath.Join(realStdlib, "json", "__init__.py")
	if err := os.WriteFile(jsonFile, []byte("# json"), 0o600); err != nil {
		t.Fatalf("write json file: %v", err)
	}

	stdlibSymlink := filepath.Join(tempDir, "stdlib-link")
	if err := os.Symlink(realStdlib, stdlibSymlink); err != nil {
		t.Fatalf("symlink stdlib: %v", err)
	}

	logicalSystemPath := filepath.Join(tempDir, "system-link")
	if err := os.Symlink(realStdlib, logicalSystemPath); err != nil {
		t.Fatalf("symlink system path: %v", err)
	}

	paths, err := buildPythonLibPaths(pythonLibDiscoveryResult{
		Stdlib:           stdlibSymlink,
		JSONOrigin:       filepath.Join(stdlibSymlink, "json", "__init__.py"),
		JSONRelativePath: filepath.Join("json", "__init__.py"),
		JSONRoot:         stdlibSymlink,
		Paths:            []string{stdlibSymlink},
	}, []string{logicalSystemPath})
	if err != nil {
		t.Fatalf("buildPythonLibPaths returned error: %v", err)
	}

	if !containsPath(paths, stdlibSymlink) {
		t.Fatalf("expected logical stdlib path %q in %v", stdlibSymlink, paths)
	}
	if containsPath(paths, realStdlib) {
		t.Fatalf("did not expect resolved stdlib path %q in %v", realStdlib, paths)
	}
	if !containsPath(paths, logicalSystemPath) {
		t.Fatalf("expected logical system path %q in %v", logicalSystemPath, paths)
	}
}

func TestBuildPythonLibPathsAcceptsStdlibZipRoot(t *testing.T) {
	tempDir := t.TempDir()
	stdlibPath := filepath.Join(tempDir, "stdlib")
	if err := os.MkdirAll(stdlibPath, 0o755); err != nil {
		t.Fatalf("mkdir stdlib: %v", err)
	}
	stdlibZipPath := filepath.Join(tempDir, "python312.zip")
	if err := os.WriteFile(stdlibZipPath, []byte("zip placeholder"), 0o600); err != nil {
		t.Fatalf("write stdlib zip: %v", err)
	}

	paths, err := buildPythonLibPaths(pythonLibDiscoveryResult{
		Stdlib:           stdlibPath,
		StdlibZipPaths:   []string{stdlibZipPath},
		JSONOrigin:       stdlibZipPath + "/json/__init__.py",
		JSONRelativePath: filepath.Join("json", "__init__.py"),
		JSONRoot:         stdlibZipPath,
		Paths:            []string{stdlibPath, stdlibZipPath},
	}, nil)
	if err != nil {
		t.Fatalf("buildPythonLibPaths returned error: %v", err)
	}
	if !containsPath(paths, stdlibZipPath) {
		t.Fatalf("expected stdlib zip root %q in %v", stdlibZipPath, paths)
	}
}

func TestPythonDiscoveryEnvClearsPythonOverrides(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("LANG", "C.UTF-8")
	t.Setenv("PYTHONPATH", "/tmp/injected")
	t.Setenv("PYTHONHOME", "/tmp/home")
	t.Setenv("PYTHONUSERBASE", "/tmp/userbase")

	env := pythonDiscoveryEnv()
	joined := strings.Join(env, "\n")

	if !strings.Contains(joined, "PATH=/usr/bin") {
		t.Fatalf("expected PATH preserved in %v", env)
	}
	for _, clearedVar := range []string{"PYTHONPATH=", "PYTHONHOME=", "PYTHONUSERBASE="} {
		if !strings.Contains(joined, clearedVar) {
			t.Fatalf("expected %s in sanitized env %v", clearedVar, env)
		}
	}
	if strings.Contains(joined, "PYTHONPATH=/tmp/injected") || strings.Contains(joined, "PYTHONHOME=/tmp/home") || strings.Contains(joined, "PYTHONUSERBASE=/tmp/userbase") {
		t.Fatalf("expected python override env vars to be cleared, got %v", env)
	}
}

func TestIsCanonicalJSONRelativePath(t *testing.T) {
	for _, path := range []string{"json.py", "json", filepath.Join("json", "__init__.py"), filepath.Join("json", "decoder.py")} {
		if !isCanonicalJSONRelativePath(path) {
			t.Fatalf("expected canonical json path %q", path)
		}
	}

	for _, path := range []string{"", filepath.Join("site-packages", "json", "__init__.py"), filepath.Join("dist-packages", "json.py"), filepath.Join("vendor", "json.py"), filepath.Join("nested", "json", "__init__.py")} {
		if isCanonicalJSONRelativePath(path) {
			t.Fatalf("expected non-canonical json path %q", path)
		}
	}
}

func mustFindTestPython(t *testing.T) string {
	t.Helper()

	for _, binary := range []string{"python3", "python"} {
		path, err := exec.LookPath(binary)
		if err == nil {
			return path
		}
	}

	t.Skip("python interpreter not available in test environment")
	return ""
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	return configPath
}
