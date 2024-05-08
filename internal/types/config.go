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
	NodejsPath    string `yaml:"nodejs_path"`
	EnableNetwork bool   `yaml:"enable_network"`
}
