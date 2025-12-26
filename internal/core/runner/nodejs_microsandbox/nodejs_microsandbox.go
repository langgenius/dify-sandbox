package nodejs_microsandbox

import (
	"context"
	"time"

	api "github.com/agent-infra/sandbox-sdk-go"
	"github.com/agent-infra/sandbox-sdk-go/client"
	"github.com/agent-infra/sandbox-sdk-go/option"
	"github.com/langgenius/dify-sandbox/internal/core/runner/types"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

type NodeJSMicroSandboxRunner struct {
	client *client.Client
}

func (n *NodeJSMicroSandboxRunner) Run(
	ctx context.Context,
	code string,
	timeout time.Duration,
	stdin []byte,
	preload string,
	options *types.RunnerOptions,
) (chan []byte, chan []byte, chan bool, error) {
	config := static.GetDifySandboxGlobalConfigurations()

	if n.client == nil {
		n.client = client.NewClient(
			option.WithBaseURL(config.Sandbox.ServerAddress),
		)
	}

	// microseconds := int(timeout.Microseconds())
	res, err := n.client.Code.ExecuteCode(ctx, &api.CodeExecuteRequest{
		Language: api.LanguageJavascript,
		Code:     code,
		//Timeout:  &microseconds,
	})
	log.Info("nodejs sandbox response is %+v", res)
	// Prepare channels
	stdoutChan := make(chan []byte, 100)
	stderrChan := make(chan []byte, 100)

	if err != nil {
		stderrChan <- []byte(err.Error())
	} else {
		success := res.GetSuccess()
		if !*success {
			var errMessage string
			for _, item := range res.Data.Outputs {
				for k, v := range item {
					if k == "evalue" {
						if innerV, ok := v.(string); ok {
							errMessage += innerV
						}
					}
				}
			}
			stderrChan <- []byte(errMessage)
		} else {
			stdoutChan <- []byte(*res.Data.GetStdout())
		}
	}

	doneChan := make(chan bool, 1)
	doneChan <- true

	return stdoutChan, stderrChan, doneChan, nil
}
