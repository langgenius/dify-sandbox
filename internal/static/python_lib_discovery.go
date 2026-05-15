package static

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const pythonLibDiscoveryScript = `import json
import os
import site
import sys
import sysconfig

paths = []

def normalize_existing(path):
    if path and os.path.isabs(path) and os.path.exists(path):
        return os.path.normpath(os.path.abspath(path))
    return ""

def add(path):
    normalized = normalize_existing(path)
    if normalized:
        paths.append(normalized)

def canonical_json_relative_path(root, origin):
    if not root or not origin:
        return ""
    try:
        relative = os.path.relpath(origin, root)
    except ValueError:
        return ""
    relative = os.path.normpath(relative)
    if relative in ("json.py", "json") or relative.startswith("json" + os.sep):
        return relative
    return ""

stdlib = sysconfig.get_path("stdlib")
platstdlib = sysconfig.get_path("platstdlib")

for key in ("stdlib", "platstdlib", "purelib", "platlib"):
    add(sysconfig.get_path(key))

for path in getattr(sys, "path", []):
    add(path)

try:
    for path in site.getsitepackages():
        add(path)
except Exception:
    pass

try:
    add(site.getusersitepackages())
except Exception:
    pass

stdlib_roots = []
for candidate in (stdlib, platstdlib):
    normalized = normalize_existing(candidate)
    if normalized:
        stdlib_roots.append({"raw": normalized, "real": os.path.realpath(candidate)})

stdlib_root_parents = set(root["real"].rsplit(os.sep, 1)[0] for root in stdlib_roots)
stdlib_zip_paths = []
for path in getattr(sys, "path", []):
    raw_path = os.path.abspath(path) if path and os.path.isabs(path) and os.path.exists(path) else ""
    normalized = normalize_existing(path)
    real_path = os.path.realpath(path) if normalized else ""
    if normalized and normalized.endswith(".zip") and real_path.rsplit(os.sep, 1)[0] in stdlib_root_parents:
        stdlib_zip_paths.append({"raw": raw_path, "real": real_path})

json_origin = getattr(json, "__file__", "") or ""
json_root = ""
json_relative_path = ""

if json_origin and os.path.isabs(json_origin):
    normalized_json_origin = os.path.realpath(json_origin) if os.path.exists(json_origin) else os.path.normpath(json_origin)
    for candidate in stdlib_roots:
        for root in (candidate["raw"], candidate["real"]):
            relative = canonical_json_relative_path(root, normalized_json_origin)
            if relative:
                json_root = candidate["raw"]
                json_relative_path = relative
                break
        if json_root:
            break

    if not json_root:
        for candidate in stdlib_zip_paths:
            for root in (candidate["raw"], candidate["real"]):
                relative = canonical_json_relative_path(root, normalized_json_origin)
                if relative:
                    json_root = candidate["raw"]
                    json_relative_path = relative
                    break
            if json_root:
                break

if json_root:
    add(json_root)

print(json.dumps({
    "paths": paths,
    "stdlib": stdlib,
    "platstdlib": platstdlib,
    "stdlib_zip_paths": [candidate["raw"] for candidate in stdlib_zip_paths],
    "json_origin": json_origin,
    "json_relative_path": json_relative_path,
    "json_root": json_root,
}))
`

type pythonLibDiscoveryResult struct {
	Paths            []string `json:"paths"`
	Stdlib           string   `json:"stdlib"`
	Platstdlib       string   `json:"platstdlib"`
	StdlibZipPaths   []string `json:"stdlib_zip_paths"`
	JSONOrigin       string   `json:"json_origin"`
	JSONRelativePath string   `json:"json_relative_path"`
	JSONRoot         string   `json:"json_root"`
}

// resolvePythonPath validates the configured python_path before discovery runs.
func resolvePythonPath(pythonPath string) (string, error) {
	if pythonPath == "" {
		return "", fmt.Errorf("python_path is empty")
	}

	if filepath.IsAbs(pythonPath) {
		info, err := os.Stat(pythonPath)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("path is a directory")
		}
		if info.Mode().Perm()&0o111 == 0 {
			return "", fmt.Errorf("path is not executable")
		}
		return filepath.Clean(pythonPath), nil
	}

	resolvedPath, err := exec.LookPath(pythonPath)
	if err != nil {
		return "", err
	}
	resolvedPath, err = filepath.Abs(resolvedPath)
	if err != nil {
		return "", err
	}

	return resolvedPath, nil
}

// discoverPythonLibPaths executes pythonPath before chroot and converts the
// interpreter's own view of stdlib/site paths into the sandbox copy list.
//
// Contract summary for future maintainers:
//   - discovery runs Python with -P and with Python override env vars cleared,
//     so results come from pythonPath rather than cwd/PYTHONPATH/PYTHONHOME;
//   - json must resolve from a canonical standard-library location, not merely
//     from any importable third-party package named json;
//   - stdlib zip roots are valid because some Python installations import
//     modules from python3x.zip;
//   - Python-discovered paths keep their logical names because the interpreter's
//     sys.path keeps those names after chroot; the copy script follows directory
//     symlinks when materializing those logical paths inside the sandbox.
func discoverPythonLibPaths(pythonPath string) ([]string, error) {
	command := exec.Command(pythonPath, "-P", "-c", pythonLibDiscoveryScript)
	command.Env = pythonDiscoveryEnv()
	output, err := command.CombinedOutput()
	if err != nil {
		trimmedOutput := strings.TrimSpace(string(output))
		if trimmedOutput == "" {
			return nil, fmt.Errorf("python discovery script failed: %w", err)
		}
		return nil, fmt.Errorf("python discovery script failed: %w: %s", err, trimmedOutput)
	}

	var result pythonLibDiscoveryResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("parse python discovery output: %w", err)
	}

	return buildPythonLibPaths(result, DEFAULT_SYSTEM_LIB_REQUIREMENTS)
}

// buildPythonLibPaths validates interpreter-reported paths and appends fixed
// system requirements used inside the sandbox.
//
// Validation intentionally treats Python and system paths differently:
//   - Python paths keep their existing logical names, because those are the
//     names already embedded in the running interpreter's sys.path;
//   - system paths preserve their logical names (for example /etc/resolv.conf)
//     so sandboxed code still sees them at the expected in-chroot locations;
//   - json is accepted only from canonical stdlib-relative locations
//     (json.py or json/...) under stdlib/platstdlib or an approved stdlib zip.
func buildPythonLibPaths(result pythonLibDiscoveryResult, systemPaths []string) ([]string, error) {
	stdlibPath, ok := normalizeExistingAbsolutePath(result.Stdlib)
	if !ok {
		return nil, fmt.Errorf("no valid standard library path discovered")
	}

	allowedJSONRoots := []string{stdlibPath}
	if platstdlibPath, ok := normalizeExistingAbsolutePath(result.Platstdlib); ok {
		allowedJSONRoots = append(allowedJSONRoots, platstdlibPath)
	}
	for _, zipPath := range result.StdlibZipPaths {
		if normalizedZipPath, ok := normalizeExistingAbsolutePath(zipPath); ok {
			allowedJSONRoots = append(allowedJSONRoots, normalizedZipPath)
		}
	}
	allowedJSONRoots = dedupePaths(allowedJSONRoots)

	jsonRootPath, ok := normalizeExistingAbsolutePath(result.JSONRoot)
	if !ok {
		return nil, fmt.Errorf("no valid standard-library json module import root discovered from %q", result.JSONOrigin)
	}
	if !containsPath(allowedJSONRoots, jsonRootPath) {
		return nil, fmt.Errorf("json module origin %q is not provided by the interpreter standard library", result.JSONOrigin)
	}
	if !isCanonicalJSONRelativePath(result.JSONRelativePath) {
		return nil, fmt.Errorf("json module origin %q is not in a canonical standard-library json location", result.JSONOrigin)
	}

	candidates := make([]string, 0, len(allowedJSONRoots)+len(result.Paths)+len(systemPaths))
	candidates = append(candidates, allowedJSONRoots...)
	candidates = append(candidates, jsonRootPath)
	for _, candidate := range result.Paths {
		normalizedPath, ok := normalizeExistingAbsolutePath(candidate)
		if !ok {
			continue
		}
		candidates = append(candidates, normalizedPath)
	}
	for _, candidate := range systemPaths {
		normalizedPath, ok := normalizeExistingAbsolutePathPreservingSymlinks(candidate)
		if !ok {
			continue
		}
		candidates = append(candidates, normalizedPath)
	}

	paths := dedupePaths(candidates)
	if !containsPath(paths, stdlibPath) {
		return nil, fmt.Errorf("discovered path list is missing standard library path %q", stdlibPath)
	}
	if !containsPath(paths, jsonRootPath) {
		return nil, fmt.Errorf("discovered path list is missing standard-library json import root %q", jsonRootPath)
	}

	return paths, nil
}

func dedupePaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))

	for _, candidate := range paths {
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		result = append(result, candidate)
	}

	return result
}

func normalizeExistingAbsolutePath(path string) (string, bool) {
	return normalizeAbsolutePath(path, false)
}

func normalizeExistingAbsolutePathPreservingSymlinks(path string) (string, bool) {
	return normalizeAbsolutePath(path, false)
}

func normalizeAbsolutePath(path string, resolveSymlinks bool) (string, bool) {
	if path == "" {
		return "", false
	}

	normalizedPath := filepath.Clean(path)
	if !filepath.IsAbs(normalizedPath) {
		return "", false
	}

	if _, err := os.Stat(normalizedPath); err != nil {
		return "", false
	}
	if !resolveSymlinks {
		return normalizedPath, true
	}

	resolvedPath, err := filepath.EvalSymlinks(normalizedPath)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(resolvedPath) {
		return "", false
	}

	return filepath.Clean(resolvedPath), true
}

func pythonDiscoveryEnv() []string {
	env := []string{}

	if path := os.Getenv("PATH"); path != "" {
		env = append(env, "PATH="+path)
	}
	if lang := os.Getenv("LANG"); lang != "" {
		env = append(env, "LANG="+lang)
	}
	if lcAll := os.Getenv("LC_ALL"); lcAll != "" {
		env = append(env, "LC_ALL="+lcAll)
	}
	if lcCType := os.Getenv("LC_CTYPE"); lcCType != "" {
		env = append(env, "LC_CTYPE="+lcCType)
	}
	if tmpDir := os.Getenv("TMPDIR"); tmpDir != "" {
		env = append(env, "TMPDIR="+tmpDir)
	}

	for _, pythonEnvVar := range []string{
		"PYTHONHOME",
		"PYTHONPATH",
		"PYTHONUSERBASE",
		"PYTHONSTARTUP",
		"PYTHONPLATLIBDIR",
		"PYTHONPYCACHEPREFIX",
		"PYTHONNOUSERSITE",
		"PYTHONWARNINGS",
		"PYTHONCASEOK",
		"PYTHONUTF8",
	} {
		env = append(env, pythonEnvVar+"=")
	}

	return env
}

func containsPath(paths []string, path string) bool {
	for _, candidate := range paths {
		if candidate == path {
			return true
		}
	}

	return false
}

func isCanonicalJSONRelativePath(path string) bool {
	if path == "" {
		return false
	}
	cleanPath := filepath.Clean(path)
	if cleanPath == "json.py" || cleanPath == "json" {
		return true
	}
	return strings.HasPrefix(cleanPath, "json"+string(os.PathSeparator))
}
