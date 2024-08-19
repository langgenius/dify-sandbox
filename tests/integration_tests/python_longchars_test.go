package integrationtests_test

import (
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestPythonLargeOutput(t *testing.T) {
	// Test case for base64
	runMultipleTestings(t, 5, func(t *testing.T) {
		resp := service.RunPython3Code(`# declare main function here
def main() -> dict:
    original_strings_with_empty = ["apple", "", "cherry", "date", "", "fig", "grape", "honeydew", "kiwi", "", "mango", "nectarine", "orange", "papaya", "quince", "raspberry", "strawberry", "tangerine", "ugli fruit", "vanilla bean", "watermelon", "xigua", "yellow passionfruit", "zucchini"] * 5

    extended_strings = []

    for s in original_strings_with_empty:
        if s: 
            repeat_times = 600
            extended_s = (s * repeat_times)[:3000]
            extended_strings.append(extended_s)
        else:
            extended_strings.append(s)
    
    return {
        "result": extended_strings,
    }

from json import loads, dumps
from base64 import b64decode

# execute main function, and return the result
# inputs is a dict, and it
inputs = b64decode('e30=').decode('utf-8')
output = main(**loads(inputs))

# convert output to json and print
output = dumps(output, indent=4)

result = f'''<<RESULT>>
{output}
<<RESULT>>'''

print(result)
		`, "", &types.RunnerOptions{
			EnableNetwork: true,
		})
		if resp.Code != 0 {
			t.Fatal(resp)
		}

		if resp.Data.(*service.RunCodeResponse).Stderr != "" {
			t.Fatalf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
		}

		if len(resp.Data.(*service.RunCodeResponse).Stdout) != 304487 {
			t.Fatalf("unexpected output, expected 304487 bytes, got %d bytes\n", len(resp.Data.(*service.RunCodeResponse).Stdout))
		}
	})
}
