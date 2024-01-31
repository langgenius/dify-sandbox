package static

import (
	"os"

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

	return nil
}

// avoid global modification, use value copy instead
func GetCoshubGlobalConfigurations() types.DifySandboxGlobalConfigurations {
	return difySandboxGlobalConfigurations
}
