package dependencies

import (
	"fmt"
	"strings"
	"sync"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
)

var preload_script_map = map[string]string{}
var preload_script_map_lock = &sync.RWMutex{}

func SetupDependency(package_name string, version string, script string) {
	preload_script_map_lock.Lock()
	defer preload_script_map_lock.Unlock()
	preload_script_map[fmt.Sprintf("%s==%s", package_name, version)] = script
}

func GetDependencies(package_name string, version string) string {
	preload_script_map_lock.RLock()
	defer preload_script_map_lock.RUnlock()
	if script, ok := preload_script_map[fmt.Sprintf("%s==%s", package_name, version)]; ok {
		return script
	}

	return ""
}

func ListDependencies() []types.Dependency {
	dependencies := []types.Dependency{}
	preload_script_map_lock.RLock()
	for k := range preload_script_map {
		parts := strings.Split(k, "==")
		package_name := ""
		version := ""
		if len(parts) == 0 {
			continue
		} else if len(parts) == 1 {
			package_name = parts[0]
		} else if len(parts) == 2 {
			package_name = parts[0]
			version = parts[1]
		}
		dependencies = append(dependencies, types.Dependency{
			Name:    package_name,
			Version: version,
		})
	}

	return dependencies
}
