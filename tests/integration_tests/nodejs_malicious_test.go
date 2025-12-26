package integrationtests_test

import (
	"context"
	"strings"
	"testing"

	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/service"
)

func TestNodejsRunCommand(t *testing.T) {
	// Test case for run_command
	resp := service.RunNodeJsCode(context.TODO(), `
const { spawn } = require( 'child_process' );
const ls = spawn( 'ls', [ '-lh', '/usr' ] );

ls.stdout.on( 'data', ( data ) => {
    console.log(data);
} );

ls.stderr.on( 'data', ( data ) => {
    console.log(data);
} );

ls.on( 'close', ( code ) => {
    console.log(code);
} );
	`, "", &types.RunnerOptions{})
	if resp.Code != 0 {
		t.Error(resp)
	}

	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stderr, "operation not permitted") {
		t.Error(resp.Data.(*service.RunCodeResponse).Stderr)
	}
}

func TestNodejsRunRedeclareFunctionCommand(t *testing.T) {
	// Test case for run_command
	resp := service.RunNodeJsCode(context.TODO(), `
var data;
function main()
{
   return {result: data};
}
function parseInt()
{
   const {execSync} = require('child_process');
   data = execSync("ls -la /", {encoding: "utf8"});
   return 0;
}
console.log(main());
	`, "", &types.RunnerOptions{})
	if resp.Code != 0 {
		t.Error(resp)
	}

	// parseInt should not be executed as it has been fixed
	if !strings.Contains(resp.Data.(*service.RunCodeResponse).Stdout, "result: undefined") {
		t.Error(resp.Data.(*service.RunCodeResponse).Stdout)
	}
}
