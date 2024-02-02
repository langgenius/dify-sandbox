package python

import (
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/langgenius/dify-sandbox/internal/core/runner"
	"github.com/langgenius/dify-sandbox/internal/static"
)

type PythonRunner struct {
	runner.Runner
	runner.SeccompRunner
}

//go:embed prescript.py
var python_sandbox_fs []byte

//go:embed python.so
var python_lib []byte

func (p *PythonRunner) Run(code string, timeout time.Duration, stdin chan []byte) (<-chan []byte, <-chan []byte, error) {
	// check if libpython.so exists
	if _, err := os.Stat("/tmp/sandbox-python/python.so"); os.IsNotExist(err) {
		err := os.MkdirAll("/tmp/sandbox-python", 0755)
		if err != nil {
			return nil, nil, err
		}
		err = os.WriteFile("/tmp/sandbox-python/python.so", python_lib, 0755)
		if err != nil {
			return nil, nil, err
		}
	}

	// create a tmp dir and copy the python script
	temp_code_name := strings.ReplaceAll(uuid.New().String(), "-", "_")
	temp_code_name = strings.ReplaceAll(temp_code_name, "/", ".")
	temp_code_path := fmt.Sprintf("/tmp/code/%s.py", temp_code_name)
	err := os.MkdirAll("/tmp/code", 0755)
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(temp_code_path)
	err = os.WriteFile(temp_code_path, []byte(code), 0755)
	if err != nil {
		return nil, nil, err
	}

	err = p.WithTempDir([]string{
		temp_code_path,
		"/tmp/sandbox-python/python.so",
	}, func() error {
		syscall.Exec("/usr/bin/python3", []string{
			"/usr/bin/python3",
			"-c",
			string(python_sandbox_fs),
			temp_code_path,
			strconv.Itoa(static.SANDBOX_USER_UID),
			strconv.Itoa(static.SANDBOX_GROUP_ID),
		}, nil)
		return nil
	})

	if err != nil {
		fmt.Println(err)
	}

	return nil, nil, nil
}
