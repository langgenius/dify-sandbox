package static

import (
	"os"
	"strconv"

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
		return err
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

	return nil
}

// avoid global modification, use value copy instead
func GetCoshubGlobalConfigurations() types.DifySandboxGlobalConfigurations {
	return difySandboxGlobalConfigurations
}
