package integrationtests_test

import (
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestNodejsBasicTemplate(t *testing.T) {
	const code = `// declare main function
function main({a}) {
	return {b: a}
}

// decode and prepare input object
var inputs_obj = JSON.parse(Buffer.from('eyJhIjoiYSJ9', 'base64').toString('utf-8'))

// execute main function
var output_obj = main(inputs_obj)

// convert output to json and print
var output_json = JSON.stringify(output_obj)
var result = ` + "`<<RESULT>>${output_json}<<RESULT>>`" + `
console.log(result)`

	runMultipleTestings(t, 30, func(t *testing.T) {
		resp := service.RunNodeJsCode(code, "", &types.RunnerOptions{
			EnableNetwork: true,
		})
		if resp.Code != 0 {
			t.Fatal(resp)
		}
	})
}

func TestNodejsBase64(t *testing.T) {
	// Test case for base64
	runMultipleTestings(t, 30, func(t *testing.T) {
		resp := service.RunNodeJsCode(`
const base64 = Buffer.from("hello world").toString("base64");
console.log(Buffer.from(base64, "base64").toString());
		`, "", &types.RunnerOptions{
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
	})
}

func TestNodejsJSON(t *testing.T) {
	// Test case for json
	runMultipleTestings(t, 30, func(t *testing.T) {
		resp := service.RunNodeJsCode(`
console.log(JSON.stringify({"hello": "world"}));
		`, "", &types.RunnerOptions{
			EnableNetwork: true,
		})
		if resp.Code != 0 {
			t.Error(resp)
		}

		if resp.Data.(*service.RunCodeResponse).Stderr != "" {
			t.Fatalf("unexpected error: %s\n", resp.Data.(*service.RunCodeResponse).Stderr)
		}

		if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, `{"hello":"world"}`) {
			t.Fatalf("unexpected output: %s\n", resp.Data.(*service.RunCodeResponse).Stdout)
		}
	})
}
