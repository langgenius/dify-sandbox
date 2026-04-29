package nodejs

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/langgenius/dify-sandbox/internal/pool"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

//go:embed pool_init_script.js
var nodePoolInitScript []byte

// nodejsPoolProcess wraps a long-lived Node.js process.
type nodejsPoolProcess struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderr     io.ReadCloser
	scriptPath string
	mu         sync.Mutex
}

// NodeJSPoolExecutor implements pool.TaskExecutor using a persistent Node.js process.
type NodeJSPoolExecutor struct {
	pool     chan *nodejsPoolProcess
	maxProcs int
	stopping bool
	mu       sync.Mutex
	procs    []*nodejsPoolProcess
}

// NewNodeJSPoolExecutor creates the executor and pre-warms processes.
func NewNodeJSPoolExecutor(maxProcs int) *NodeJSPoolExecutor {
	if maxProcs <= 0 {
		maxProcs = 1
	}
	e := &NodeJSPoolExecutor{
		pool:     make(chan *nodejsPoolProcess, maxProcs),
		maxProcs: maxProcs,
	}

	for i := 0; i < maxProcs; i++ {
		proc, err := e.startProcess()
		if err != nil {
			log.Error("nodejs pool: failed to start process %d: %v", i, err)
			continue
		}
		e.procs = append(e.procs, proc)
		e.pool <- proc
	}

	log.Info("nodejs pool: ready with %d process(es)", len(e.procs))
	return e
}

func (e *NodeJSPoolExecutor) startProcess() (*nodejsPoolProcess, error) {
	cfg := static.GetDifySandboxGlobalConfigurations()

	tmp, err := os.CreateTemp("", "dify_nodejs_pool_*.js")
	if err != nil {
		return nil, fmt.Errorf("nodejs pool: create temp script: %w", err)
	}
	scriptPath := tmp.Name()
	if _, err = tmp.Write(nodePoolInitScript); err != nil {
		tmp.Close()
		os.Remove(scriptPath)
		return nil, fmt.Errorf("nodejs pool: write temp script: %w", err)
	}
	tmp.Close()

	cmd := exec.Command(cfg.NodejsPath, scriptPath)
	cmd.Env = append(os.Environ())

	stdin, err := cmd.StdinPipe()
	if err != nil {
		os.Remove(scriptPath)
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		os.Remove(scriptPath)
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		os.Remove(scriptPath)
		return nil, err
	}

	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		os.Remove(scriptPath)
		return nil, err
	}

	proc := &nodejsPoolProcess{
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		stderr:     stderr,
		scriptPath: scriptPath,
	}

	// Wait for the ready signal.
	readyCh := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(proc.stderr)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "NODEJS_POOL_READY") {
				close(readyCh)
				return
			}
		}
	}()

	select {
	case <-readyCh:
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		stdin.Close()
		stdout.Close()
		stderr.Close()
		os.Remove(scriptPath)
		return nil, fmt.Errorf("nodejs pool: process start timeout")
	}

	return proc, nil
}

// Execute implements pool.TaskExecutor.
func (e *NodeJSPoolExecutor) Execute(task *pool.PoolTask) *pool.PoolResult {
	var proc *nodejsPoolProcess
	select {
	case proc = <-e.pool:
	default:
		var err error
		proc, err = e.startProcess()
		if err != nil {
			return &pool.PoolResult{Error: err}
		}
	}

	key := make([]byte, 16)
	rand.Read(key) //nolint:errcheck

	enc := func(s string) string {
		b := []byte(s)
		out := make([]byte, len(b))
		for i := range b {
			out[i] = b[i] ^ key[i%len(key)]
		}
		return base64.StdEncoding.EncodeToString(out)
	}

	payload := map[string]interface{}{
		"code":           enc(task.Code),
		"key":            base64.StdEncoding.EncodeToString(key),
		"enable_network": task.Options != nil && task.Options.EnableNetwork,
	}
	if task.Preload != "" {
		payload["preload"] = enc(task.Preload)
	}

	cmdJSON, err := json.Marshal(payload)
	if err != nil {
		e.returnProcess(proc)
		return &pool.PoolResult{Error: err}
	}

	stdoutCh := make(chan []byte, 1)
	stderrCh := make(chan []byte, 1)
	doneCh := make(chan bool, 1)

	timeout := task.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	go func() {
		defer func() {
			close(stdoutCh)
			close(stderrCh)
			doneCh <- true
			close(doneCh)
			e.returnProcess(proc)
		}()

		proc.mu.Lock()
		proc.stdin.Write(append(cmdJSON, '\n')) //nolint:errcheck
		proc.mu.Unlock()

		// Read one response line from stdout.
		respCh := make(chan []byte, 1)
		errCh := make(chan error, 1)
		go func() {
			reader := bufio.NewReader(proc.stdout)
			line, readErr := reader.ReadBytes('\n')
			if readErr != nil {
				errCh <- readErr
				return
			}
			respCh <- bytes.TrimSpace(line)
		}()

		select {
		case line := <-respCh:
			var resp struct {
				Stdout string `json:"stdout"`
				Stderr string `json:"stderr"`
				Error  string `json:"error"`
			}
			if jsonErr := json.Unmarshal(line, &resp); jsonErr != nil {
				stdoutCh <- line
				return
			}
			if resp.Stdout != "" {
				stdoutCh <- []byte(resp.Stdout)
			}
			combined := resp.Stderr
			if resp.Error != "" {
				if combined != "" {
					combined += "\n"
				}
				combined += resp.Error
			}
			if combined != "" {
				stderrCh <- []byte(combined)
			}

		case readErr := <-errCh:
			stderrCh <- []byte(readErr.Error())

		case <-time.After(timeout + 2*time.Second):
			stderrCh <- []byte(fmt.Sprintf("nodejs pool: execution timeout after %v", timeout))
		}
	}()

	return &pool.PoolResult{
		Stdout: stdoutCh,
		Stderr: stderrCh,
		Done:   doneCh,
	}
}

func (e *NodeJSPoolExecutor) returnProcess(proc *nodejsPoolProcess) {
	if e.stopping {
		e.closeProcess(proc)
		return
	}
	select {
	case e.pool <- proc:
	default:
		e.closeProcess(proc)
	}
}

func (e *NodeJSPoolExecutor) closeProcess(proc *nodejsPoolProcess) {
	proc.stdin.Close()
	proc.stdout.Close()
	proc.stderr.Close()
	if proc.cmd.Process != nil {
		proc.cmd.Process.Kill()
		proc.cmd.Wait() //nolint:errcheck
	}
	if proc.scriptPath != "" {
		os.Remove(proc.scriptPath)
	}
}

// Shutdown implements pool.TaskExecutor.
func (e *NodeJSPoolExecutor) Shutdown() {
	e.mu.Lock()
	e.stopping = true
	e.mu.Unlock()

	close(e.pool)
	for proc := range e.pool {
		e.closeProcess(proc)
	}
	log.Info("nodejs pool: shutdown complete")
}
