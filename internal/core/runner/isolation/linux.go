//go:build linux

package isolation

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func setNamespaces(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWIPC | syscall.CLONE_NEWUTS | syscall.CLONE_NEWNET, Pdeathsig: syscall.SIGKILL}
}
func (e *Execution) AttachCgroup(pid int) error {
	// Only a pre-delegated subtree is accepted. No host hierarchy is discovered,
	// mounted, or made writable by the supervisor.
	var fs unix.Statfs_t
	if err := unix.Statfs(e.profile.CgroupRoot, &fs); err != nil {
		return err
	}
	if fs.Type != unix.CGROUP2_SUPER_MAGIC {
		return fmt.Errorf("delegated cgroup v2 filesystem required")
	}
	if _, err := os.Stat(filepath.Join(e.profile.CgroupRoot, "cgroup.controllers")); err != nil {
		return fmt.Errorf("delegated cgroup v2 required: %w", err)
	}
	e.cgroup = filepath.Join(e.profile.CgroupRoot, filepath.Base(e.WorkDir))
	if err := os.Mkdir(e.cgroup, 0700); err != nil {
		return err
	}
	b := e.spec.Budget
	for _, control := range [][2]string{{"memory.max", strconv.FormatInt(b.MemoryBytes, 10)}, {"memory.swap.max", "0"}, {"memory.oom.group", "1"}, {"pids.max", strconv.FormatInt(b.PIDs, 10)}, {"cpu.max", fmt.Sprintf("%d %d", b.CPUQuotaMicros, b.CPUPeriodMicros)}, {"cgroup.procs", strconv.Itoa(pid)}} {
		if err := os.WriteFile(filepath.Join(e.cgroup, control[0]), []byte(control[1]), 0600); err != nil {
			return fmt.Errorf("apply %s: %w", control[0], err)
		}
	}
	return nil
}
func (e *Execution) cleanup() error {
	if e.cgroup != "" {
		if err := os.WriteFile(filepath.Join(e.cgroup, "cgroup.kill"), []byte("1"), 0600); err != nil {
			return err
		}
		deadline := time.Now().Add(time.Duration(e.spec.Budget.CleanupMillis) * time.Millisecond)
		for {
			events, err := os.ReadFile(filepath.Join(e.cgroup, "cgroup.events"))
			if err != nil {
				return err
			}
			if strings.Contains(string(events), "populated 0") {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("cgroup still populated")
			}
			time.Sleep(time.Millisecond)
		}
		if err := os.Remove(e.cgroup); err != nil {
			return err
		}
	}
	if e.WorkDir != "" {
		return os.RemoveAll(e.WorkDir)
	}
	return nil
}
func (e *Execution) readUsage() Usage {
	var u Usage
	if e.cgroup == "" {
		return u
	}
	peak, _ := os.ReadFile(filepath.Join(e.cgroup, "memory.peak"))
	u.MemoryPeakBytes, _ = strconv.ParseInt(strings.TrimSpace(string(peak)), 10, 64)
	cpu, _ := os.ReadFile(filepath.Join(e.cgroup, "cpu.stat"))
	fields := strings.Fields(string(cpu))
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "usage_usec" {
			u.CPUTimeMicros, _ = strconv.ParseInt(fields[i+1], 10, 64)
		}
	}
	return u
}

type workerConfig struct {
	Spec                  InvocationSpec
	EnvironmentRoot, Root string
	Launch                Launch
	Payload               Payload
	Env                   []string
}

// WorkerMain is only an isolation wrapper. The command and bootstrap are
// produced by the existing Python/Node runners and arrive on a private pipe.
func WorkerMain() error {
	gate := os.NewFile(3, "start-gate")
	if gate == nil {
		return fmt.Errorf("missing start gate")
	}
	var token [1]byte
	_, err := io.ReadFull(gate, token[:])
	gate.Close()
	if err != nil || token[0] != 1 {
		return fmt.Errorf("start gate refused")
	}
	var cfg workerConfig
	if err = json.NewDecoder(os.Stdin).Decode(&cfg); err != nil {
		return err
	}
	if err = cfg.Spec.Budget.Validate(); err != nil {
		return err
	}
	if err = applyRlimits(cfg.Spec.Budget); err != nil {
		return err
	}
	if err = unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		return err
	}
	if err = unix.Mount(cfg.EnvironmentRoot, cfg.Root, "", unix.MS_BIND, ""); err != nil {
		return err
	}
	// Recursive mount attributes avoid writable nested mounts in an environment.
	if err = unix.MountSetattr(unix.AT_FDCWD, cfg.Root, unix.AT_RECURSIVE, &unix.MountAttr{Attr_set: unix.MOUNT_ATTR_RDONLY | unix.MOUNT_ATTR_NOSUID | unix.MOUNT_ATTR_NODEV}); err != nil {
		return err
	}
	work := filepath.Join(cfg.Root, "work")
	if err = unix.Mount("tmpfs", work, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, fmt.Sprintf("size=%d,mode=0777", cfg.Spec.Budget.DiskBytes)); err != nil {
		return err
	}
	if err = unix.Chroot(cfg.Root); err != nil {
		return err
	}
	if err = os.Chdir("/"); err != nil {
		return err
	}
	if err = os.Mkdir("/work/tmp", 0777); err != nil {
		return err
	}
	if err = os.Chmod("/work/tmp", 0777); err != nil {
		return err
	}
	if cfg.Launch.BootstrapPath != "/work/bootstrap" {
		return fmt.Errorf("invalid native bootstrap path")
	}
	if err = os.WriteFile(cfg.Launch.BootstrapPath, []byte(cfg.Launch.Bootstrap), 0444); err != nil {
		return err
	}
	// Preload executes under the same native seccomp/UID boundary as submitted code.
	code := cfg.Payload.Preload + "\n" + cfg.Payload.Code
	if err = os.WriteFile("/work/code", []byte(code), 0444); err != nil {
		return err
	}
	codeFile, err := os.Open("/work/code")
	if err != nil {
		return err
	}
	if int(codeFile.Fd()) != 3 {
		if err = unix.Dup3(int(codeFile.Fd()), 3, 0); err != nil {
			return err
		}
		codeFile.Close()
	} else {
		unix.CloseOnExec(3)
		if _, err = unix.FcntlInt(3, unix.F_SETFD, 0); err != nil {
			return err
		}
	}
	// Replace the configuration pipe with an empty request-local stdin.
	if err = os.WriteFile("/work/stdin", nil, 0444); err != nil {
		return err
	}
	empty, err := os.Open("/work/stdin")
	if err != nil {
		return err
	}
	if err = unix.Dup3(int(empty.Fd()), 0, 0); err != nil {
		empty.Close()
		return err
	}
	empty.Close()
	// Descriptor 3 is the only inherited non-stdio descriptor.
	if err = unix.CloseRange(4, ^uint(0), unix.CLOSE_RANGE_CLOEXEC); err != nil {
		return err
	}
	if err = unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	return syscall.Exec(cfg.Launch.Executable, append([]string{cfg.Launch.Executable}, cfg.Launch.Args...), cfg.Env)
}
func applyRlimits(b Budget) error {
	for _, v := range []struct {
		resource int
		limit    uint64
	}{{unix.RLIMIT_NOFILE, uint64(b.FDs)}, {unix.RLIMIT_FSIZE, uint64(b.DiskBytes)}, {unix.RLIMIT_CPU, uint64((b.CPUTimeMillis + 999) / 1000)}, {unix.RLIMIT_CORE, 0}} {
		if err := unix.Setrlimit(v.resource, &unix.Rlimit{Cur: v.limit, Max: v.limit}); err != nil {
			return err
		}
	}
	return nil
}

func (e *Execution) observeStart() error {
	o := Observation{ExecutionID: e.spec.ExecutionID, Phase: "gated", Root: e.WorkDir, Cgroup: e.cgroup, Namespaces: map[string]string{}, Controls: map[string]string{}}
	for _, ns := range []string{"mnt", "pid", "ipc", "uts", "net"} {
		id, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/%s", e.cmd.Process.Pid, ns))
		if err != nil {
			return err
		}
		o.Namespaces[ns] = id
	}
	for _, control := range []string{"memory.max", "memory.swap.max", "memory.oom.group", "pids.max", "cpu.max"} {
		value, err := os.ReadFile(filepath.Join(e.cgroup, control))
		if err != nil {
			return err
		}
		o.Controls[control] = strings.TrimSpace(string(value))
	}
	e.observation = o
	if e.observer != nil {
		e.observer(o)
	}
	return nil
}
func (e *Execution) monitorCPU(done <-chan struct{}, stopped chan<- struct{}, exceeded chan<- struct{}) {
	defer close(stopped)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if e.readUsage().CPUTimeMicros > e.spec.Budget.CPUTimeMillis*1000 {
				exceeded <- struct{}{}
				_ = os.WriteFile(filepath.Join(e.cgroup, "cgroup.kill"), []byte("1"), 0600)
				return
			}
		}
	}
}

func (e *Execution) observeEvents() {
	if e.observation.Controls == nil {
		return
	}
	for _, name := range []string{"memory.events", "pids.events"} {
		value, err := os.ReadFile(filepath.Join(e.cgroup, name))
		if err == nil {
			e.observation.Controls[name] = strings.TrimSpace(string(value))
		}
	}
}
