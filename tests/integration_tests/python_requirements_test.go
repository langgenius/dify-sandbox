package integrationtests_test

import (
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
	"strings"
	"testing"
)

func TestPyModuleAutoImport(t *testing.T) {
	code :=
		`
# declare main function

import progress
def main(arg1: int, arg2: int) -> dict:
        print(pandas.__version__)
        print(os.environ)
        print(sys.path)
        return {
                "result": arg1 + arg2,
        }
import json
from base64 import b64decode

# decode and prepare input dict
inputs_obj = json.loads(b64decode('eyJhcmcxIjogIjEyMSIsICJhcmcyIjogIjEyMSJ9').decode('utf-8'))

# execute main function
output_obj = main(**inputs_obj)

# convert output to json and print
output_json = json.dumps(output_obj, indent=4)
result = f'''<<RESULT>>{output_json}<<RESULT>>'''
print(result)
`
	log.Info("code:%v\n", code)
	configurations := static.GetDifySandboxGlobalConfigurations()
	configurations.PythonModuleAutoImport = true
	configurations.EnableNetwork = true
	resp := service.RunPython3Code(code, "", &types.RunnerOptions{
		EnableNetwork: true,
	})
	if resp.Code != 0 {
		t.Fatal(resp)
	}

	if resp.Data.(*service.RunCodeResponse).Stderr != "" {
		t.Fatalf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, "hello world") {
		t.Fatalf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
	}
}
