package preload

import (
	"fmt"
	"sync"
)

var preload_script_map = map[string]string{}
var preload_script_map_lock = &sync.RWMutex{}

func SetupDependency(package_name string, version string, script string) {
	preload_script_map_lock.Lock()
	defer preload_script_map_lock.Unlock()
	preload_script_map[package_name] = script
}

func GetDependencies(package_name string, version string) string {
	preload_script_map_lock.RLock()
	defer preload_script_map_lock.RUnlock()
	if script, ok := preload_script_map[package_name]; ok {
		return script
	}

	return fmt.Sprintf("import %s", package_name)
}
