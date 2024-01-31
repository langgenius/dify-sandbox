package types

type DifySandboxGlobalConfigurations struct {
	App struct {
		Port  int  `yaml:"port"`
		Debug bool `yaml:"debug"`
	} `yaml:"app"`
}
