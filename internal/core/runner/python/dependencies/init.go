package dependencies

import (
	"sync"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
)

var preload_script_map = map[string]string{}
var preload_script_map_lock = &sync.RWMutex{}

func SetupDependency(package_name string, version string) {
	preload_script_map_lock.Lock()
	defer preload_script_map_lock.Unlock()
	preload_script_map[package_name] = version
}

func GetDependency(package_name string, version string) string {
	preload_script_map_lock.RLock()
	defer preload_script_map_lock.RUnlock()
	return preload_script_map[package_name]
}

func ListDependencies() []types.Dependency {
	dependencies := []types.Dependency{}
	preload_script_map_lock.RLock()
	defer preload_script_map_lock.RUnlock()
	for package_name, version := range preload_script_map {
		dependencies = append(dependencies, types.Dependency{
			Name:    package_name,
			Version: version,
		})
	}

	return dependencies
}
