package cases

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
)

// ResourceRecorder is trusted fixture-only instrumentation. It reads only PIDs
// currently listed by an owned delegated cgroup; no user-provided path is used.
// FD/disk values are sampled lower bounds, with the sampling period disclosed.
type ResourceSample struct {
	PIDs               int64  `json:"pids"`
	FDs                int64  `json:"fds"`
	DiskBytes          int64  `json:"disk_bytes"`
	Samples            int64  `json:"samples"`
	NativeSamples      int64  `json:"native_samples"`
	NativeCapabilities uint64 `json:"native_capabilities"`
	IntervalMicros     int64  `json:"sample_interval_micros"`
	Error              string `json:"error,omitempty"`
}
type resourceEntry struct {
	stop, done chan struct{}
	sample     ResourceSample
	ready      atomic.Bool
	line       []byte
}
type ResourceRecorder struct {
	mu        sync.Mutex
	directory string
	entries   map[string]*resourceEntry
	limit     int
}

func NewResourceRecorder(directory string, limit int) *ResourceRecorder {
	return &ResourceRecorder{directory: directory, entries: map[string]*resourceEntry{}, limit: limit}
}
func (r *ResourceRecorder) Observe(o isolation.Observation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if o.Phase == "gated" {
		if len(r.entries) >= r.limit {
			return fmt.Errorf("resource evidence capacity exhausted")
		}
		if _, ok := r.entries[o.ExecutionID]; ok {
			return fmt.Errorf("duplicate resource observation")
		}
		entry := &resourceEntry{stop: make(chan struct{}), done: make(chan struct{}), sample: ResourceSample{IntervalMicros: 2000}}
		r.entries[o.ExecutionID] = entry
		go func() {
			defer close(entry.done)
			ticker := time.NewTicker(2 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-entry.stop:
					return
				case <-ticker.C:
					sampleResources(o.Cgroup, &entry.sample, entry.ready.Load())
				}
			}
		}()
	}
	if o.Phase == "stdout" {
		if entry := r.entries[o.ExecutionID]; entry != nil {
			for _, b := range []byte(o.StdoutChunk) {
				if b == '\n' {
					if string(entry.line) == "CS1_READY" {
						entry.ready.Store(true)
					}
					entry.line = entry.line[:0]
				} else if len(entry.line) < 128 {
					entry.line = append(entry.line, b)
				}
			}
		}
	}
	if o.Phase == "completed" {
		entry := r.entries[o.ExecutionID]
		if entry == nil {
			return nil
		}
		close(entry.stop)
		<-entry.done
		if entry.sample.Samples == 0 {
			entry.sample.Error = "no independent FD/disk sample; required proc/user-namespace access unavailable"
		}
		raw, err := json.Marshal(entry.sample)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(r.directory, o.ExecutionID+".resources.json"), raw, 0444)
	}
	return nil
}
func RequireNativeResourceSample(sample ResourceSample) error {
	if sample.Error != "" || sample.NativeSamples <= 0 || sample.Samples < sample.NativeSamples || sample.NativeCapabilities != 0 || sample.IntervalMicros <= 0 || sample.PIDs <= 0 || sample.FDs <= 0 || sample.DiskBytes <= 0 {
		return fmt.Errorf("native resource/capability evidence unavailable or unsafe: %+v", sample)
	}
	return nil
}
func sampleResources(cgroup string, sample *ResourceSample, native bool) {
	raw, err := os.ReadFile(filepath.Join(cgroup, "pids.current"))
	if err == nil {
		value, _ := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if value > sample.PIDs {
			sample.PIDs = value
		}
	}
	raw, err = os.ReadFile(filepath.Join(cgroup, "cgroup.procs"))
	if err != nil {
		return
	}
	var fds, disk int64
	valid := false
	var capabilities uint64
	for _, value := range strings.Fields(string(raw)) {
		pid, err := strconv.Atoi(value)
		if err != nil || pid <= 1 {
			continue
		}
		proc := fmt.Sprintf("/proc/%d", pid)
		membership, err := os.ReadFile(filepath.Join(proc, "cgroup"))
		if err != nil || !strings.HasSuffix(strings.TrimSpace(string(membership)), "/"+filepath.Base(cgroup)) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(proc, "fd"))
		if err != nil {
			continue
		}
		status, err := os.ReadFile(filepath.Join(proc, "status"))
		if err != nil {
			continue
		}
		fields := map[string]bool{}
		var caps uint64
		for _, line := range strings.Split(string(status), "\n") {
			parts := strings.Fields(line)
			if len(parts) != 2 {
				continue
			}
			if parts[0] == "CapEff:" || parts[0] == "CapPrm:" || parts[0] == "CapAmb:" {
				value, err := strconv.ParseUint(parts[1], 16, 64)
				if err == nil {
					fields[parts[0]] = true
					caps |= value
				}
			}
		}
		if len(fields) != 3 {
			continue
		}
		var stat syscall.Statfs_t
		if err = syscall.Statfs(filepath.Join(proc, "root/work"), &stat); err != nil || stat.Type != 0x01021994 {
			continue
		} // actual tmpfs
		fds += int64(len(files))
		used := (int64(stat.Blocks) - int64(stat.Bfree)) * stat.Bsize
		if used > disk {
			disk = used
		}
		valid = true
		capabilities |= caps
	}
	if valid {
		sample.Samples++
		if native {
			sample.NativeSamples++
			sample.NativeCapabilities |= capabilities
		}
		if fds > sample.FDs {
			sample.FDs = fds
		}
		if disk > sample.DiskBytes {
			sample.DiskBytes = disk
		}
	}
}
