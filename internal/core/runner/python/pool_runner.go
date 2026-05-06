package python

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

//go:embed pool_init_script.py
var poolInitScript []byte

// pythonPoolProcess wraps a long-lived Python process.
type pythonPoolProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	scriptPath string
	reqCh      chan *poolTask
}

type poolTask struct {
	command []byte
	respCh  chan []byte
	errCh   chan error
}

// PythonPoolExecutor implements pool.TaskExecutor using a persistent process.
type PythonPoolExecutor struct {
	pool     chan *pythonPoolProcess
	maxProcs int
	stopping bool
	mu       sync.Mutex
}

// NewPythonPoolExecutor creates the executor and pre-warms one process.
func NewPythonPoolExecutor(maxProcs int) *PythonPoolExecutor {
	if maxProcs <= 0 {
		maxProcs = 1
	}
	e := &PythonPoolExecutor{
		pool:     make(chan *pythonPoolProcess, maxProcs),
		maxProcs: maxProcs,
	}

	proc, err := e.startProcess()
	if err != nil {
		log.Error("python pool: failed to pre-warm process: %v", err)
		return e
	}
	e.pool <- proc
	log.Info("python pool: ready with %d pre-warmed process(es)", 1)
	return e
}

func (e *PythonPoolExecutor) startProcess() (*pythonPoolProcess, error) {
	cfg := static.GetDifySandboxGlobalConfigurations()

	// Write embedded init script to a temp file.
	tmp, err := os.CreateTemp("", "dify_python_pool_*.py")
	if err != nil {
		return nil, fmt.Errorf("python pool: create temp script: %w", err)
	}
	scriptPath := tmp.Name()
	if _, err = tmp.Write(poolInitScript); err != nil {
		tmp.Close()
		os.Remove(scriptPath)
		return nil, fmt.Errorf("python pool: write temp script: %w", err)
	}
	tmp.Close()

	cmd := exec.Command(cfg.PythonPath, "-u", scriptPath)
	cmd.Dir = LIB_PATH
	cmd.Env = []string{
		fmt.Sprintf("SANDBOX_UID=%d", static.SANDBOX_USER_UID),
		fmt.Sprintf("SANDBOX_GID=%d", static.SANDBOX_GROUP_ID),
	}

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

	proc := &pythonPoolProcess{
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		stderr:     stderr,
		scriptPath: scriptPath,
		reqCh:      make(chan *poolTask, 64),
	}

	// Wait for the ready signal on stderr.
	readyCh := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(proc.stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "PYTHON_POOL_READY") {
				close(readyCh)
				return
			}
		}
	}()

	select {
	case <-readyCh:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		stdin.Close()
		stdout.Close()
		stderr.Close()
		os.Remove(scriptPath)
		return nil, fmt.Errorf("python pool: process start timeout")
	}

	go proc.loop()
	return proc, nil
}

// loop is the single stdout-reader goroutine for this process.
func (p *pythonPoolProcess) loop() {
	reader := bufio.NewReader(p.stdout)
	for task := range p.reqCh {
		if _, err := p.stdin.Write(task.command); err != nil {
			task.errCh <- fmt.Errorf("python pool: stdin write: %w", err)
			continue
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			task.errCh <- fmt.Errorf("python pool: stdout read: %w", err)
			continue
		}
		task.respCh <- bytes.TrimSpace(line)
	}
}

// Execute implements pool.TaskExecutor.
func (e *PythonPoolExecutor) Execute(task *pool.PoolTask) *pool.PoolResult {
	// Acquire a process.
	var proc *pythonPoolProcess
	select {
	case proc = <-e.pool:
	default:
		var err error
		proc, err = e.startProcess()
		if err != nil {
			return &pool.PoolResult{Error: err}
		}
	}

	// Encrypt code.
	key := make([]byte, 32)
	rand.Read(key) //nolint:errcheck
	encrypted := make([]byte, len(task.Code))
	for i := range task.Code {
		encrypted[i] = task.Code[i] ^ key[i%len(key)]
	}

	payload := map[string]interface{}{
		"code":           base64.StdEncoding.EncodeToString(encrypted),
		"key":            base64.StdEncoding.EncodeToString(key),
		"enable_network": task.Options != nil && task.Options.EnableNetwork,
	}

	if task.Preload != "" {
		encPre := make([]byte, len(task.Preload))
		for i := range task.Preload {
			encPre[i] = task.Preload[i] ^ key[i%len(key)]
		}
		payload["preload"] = base64.StdEncoding.EncodeToString(encPre)
	}

	cmdJSON, err := json.Marshal(payload)
	if err != nil {
		e.returnProcess(proc)
		return &pool.PoolResult{Error: err}
	}

	pt := &poolTask{
		command: append(cmdJSON, '\n'),
		respCh:  make(chan []byte, 1),
		errCh:   make(chan error, 1),
	}

	select {
	case proc.reqCh <- pt:
	case <-time.After(5 * time.Second):
		e.returnProcess(proc)
		return &pool.PoolResult{Error: fmt.Errorf("python pool: enqueue timeout")}
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

		select {
		case line := <-pt.respCh:
			if len(line) == 0 {
				stderrCh <- []byte("empty response from python pool worker")
				return
			}
			// Parse and relay stdout/stderr from the JSON response.
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

		case execErr := <-pt.errCh:
			stderrCh <- []byte(execErr.Error())

		case <-time.After(timeout + 2*time.Second):
			stderrCh <- []byte(fmt.Sprintf("python pool: execution timeout after %v", timeout))
		}
	}()

	return &pool.PoolResult{
		Stdout: stdoutCh,
		Stderr: stderrCh,
		Done:   doneCh,
	}
}

func (e *PythonPoolExecutor) returnProcess(proc *pythonPoolProcess) {
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

func (e *PythonPoolExecutor) closeProcess(proc *pythonPoolProcess) {
	close(proc.reqCh)
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
func (e *PythonPoolExecutor) Shutdown() {
	e.mu.Lock()
	e.stopping = true
	e.mu.Unlock()

	close(e.pool)
	for proc := range e.pool {
		e.closeProcess(proc)
	}
	log.Info("python pool: shutdown complete")
}
