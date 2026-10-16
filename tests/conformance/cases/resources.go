package cases

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
)

// All identity data comes from the trusted observer's proc view, never stdout.
// READY only selects the post-bootstrap sampling interval for fixed workloads.
type ProcessIdentity struct {
	PID, TGID, NamespacePID                      int
	StartTime, UserNS, PIDNS, Cgroup, Executable string
	UIDs, GIDs                                   [4]int
	CapEff, CapPrm, CapAmb                       uint64
}
type NativeCustody struct {
	Language, Mode        string
	HostUID, HostGID      int
	UserNS, PIDNS, Cgroup string
	GatedPayload, Payload ProcessIdentity
	RequiredThreads       []int
	Threads               []ProcessIdentity
	ObservedAt            int64
}
type ResourceSample struct {
	PIDs               int64                `json:"pids"`
	FDs                int64                `json:"fds"`
	DiskBytes          int64                `json:"disk_bytes"`
	Samples            int64                `json:"samples"`
	NativeSamples      int64                `json:"native_samples"`
	NativeCapabilities uint64               `json:"native_capabilities"`
	IntervalMicros     int64                `json:"sample_interval_micros"`
	Error              string               `json:"error,omitempty"`
	Custody            *NativeCustody       `json:"custody,omitempty"`
	Sentinel           *SentinelObservation `json:"sentinel,omitempty"`
}
type resourceEntry struct {
	stop, done     chan struct{}
	sample         ResourceSample
	gate           isolation.Observation
	payload        ProcessIdentity
	language, mode string
	ready          bool
	line           []byte
	discard        bool
	sentinel       bool
	signalSent     bool
	signalAt       int64
}
type ResourceRecorder struct {
	mu        sync.Mutex
	directory string
	entries   map[string]*resourceEntry
	expected  map[string]isolation.InvocationSpec
	limit     int
	proc      procReader
}
type procReader struct{ root string }

func NewResourceRecorder(directory string, limit int) *ResourceRecorder {
	return &ResourceRecorder{directory: directory, entries: map[string]*resourceEntry{}, expected: map[string]isolation.InvocationSpec{}, limit: limit, proc: procReader{root: "/proc"}}
}

// Expect is called by the trusted launch factory with the accepted invocation.
func (r *ResourceRecorder) Expect(s isolation.InvocationSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.expected) >= r.limit {
		return fmt.Errorf("bounded resource expectation capacity exhausted")
	}
	if s.Language != "python3" && s.Language != "nodejs" {
		return fmt.Errorf("unsupported native language")
	}
	if s.Network.Mode != "none" && s.Network.Mode != "scoped" {
		return fmt.Errorf("unsupported native scope")
	}
	r.expected[s.ExecutionID] = s
	return nil
}
func (r *ResourceRecorder) Observe(o isolation.Observation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.entries[o.ExecutionID]
	if o.Phase == "gated" {
		if e != nil || len(r.entries) >= r.limit {
			return fmt.Errorf("duplicate/exhausted resource evidence")
		}
		spec, ok := r.expected[o.ExecutionID]
		if !ok {
			return fmt.Errorf("missing trusted invocation expectation")
		}
		o.Controls = cloneStrings(o.Controls)
		o.Namespaces = cloneStrings(o.Namespaces)
		e = &resourceEntry{stop: make(chan struct{}), done: make(chan struct{}), gate: o, language: spec.Language, mode: spec.Network.Mode, sample: ResourceSample{IntervalMicros: 2000}}
		r.entries[o.ExecutionID] = e
		if err := requireGatedCustody(o); err != nil {
			e.sample.Error = err.Error()
		} else {
			var err error
			e.payload, err = r.proc.gatedPayload(o)
			if err != nil {
				e.sample.Error = err.Error()
			}
		}
		go r.poll(o.ExecutionID, e)
	}
	if e == nil {
		return nil
	}
	if o.Phase == "stdout" {
		for _, b := range []byte(o.StdoutChunk) {
			if b == '\n' {
				if !e.discard {
					if string(e.line) == "CS1_READY" {
						e.ready = true
					}
					if string(e.line) == "CS5_CUSTODY_VICTIM_READY" && strings.HasPrefix(o.ExecutionID, "cs1-custody-victim-") {
						e.sentinel = true
					}
				}
				e.line = e.line[:0]
				e.discard = false
			} else if !e.discard {
				if len(e.line) >= 64 {
					e.discard = true
					e.line = nil
				} else {
					e.line = append(e.line, b)
				}
			}
		}
	}
	if o.Phase == "completed" {
		close(e.stop)
		// poll takes the same lock; it checks stop before obtaining the lock.
		if e.sample.NativeSamples == 0 && e.sample.Error == "" {
			e.sample.Error = "no independently inspected live payload after readiness"
		}
		return r.write(o.ExecutionID, e.sample)
	}
	return nil
}
func (r *ResourceRecorder) poll(id string, e *resourceEntry) {
	defer close(e.done)
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-ticker.C:
			r.mu.Lock()
			select {
			case <-e.stop:
				r.mu.Unlock()
				return
			default:
			}
			if e.ready && e.sample.Error == "" {
				frame, fds, disk, pids, err := r.proc.capture(e.gate, e.payload, e.language, e.mode)
				if err != nil {
					e.sample.Error = err.Error()
				} else if frame != nil {
					e.sample.Custody = frame
					e.sample.Samples++
					e.sample.NativeSamples++
					e.sample.PIDs = max64(e.sample.PIDs, pids)
					e.sample.FDs = max64(e.sample.FDs, fds)
					e.sample.DiskBytes = max64(e.sample.DiskBytes, disk)
					if e.sentinel {
						if err = r.observeSentinels(id, e); err != nil {
							e.sample.Error = err.Error()
						}
					}
				}
				if err = r.write(id, e.sample); err != nil {
					e.sample.Error = err.Error()
				}
			}
			r.mu.Unlock()
		}
	}
}
func (r *ResourceRecorder) write(id string, s ResourceSample) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(r.directory, ".resources-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err == nil {
		err = f.Chmod(0444)
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(r.directory, id+".resources.json"))
}
func mappedHost(mapping string, container int) (int, error) {
	for _, line := range strings.Split(mapping, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		inside, e1 := strconv.Atoi(fields[0])
		host, e2 := strconv.Atoi(fields[1])
		size, e3 := strconv.Atoi(fields[2])
		if e1 == nil && e2 == nil && e3 == nil && inside == container && size == 1 && host >= 0 {
			return host, nil
		}
	}
	return 0, fmt.Errorf("required single-ID namespace mapping missing")
}
func requireGatedCustody(o isolation.Observation) error {
	for _, name := range []string{"mnt", "pid", "ipc", "uts", "net", "user"} {
		if o.Namespaces[name] == "" {
			return fmt.Errorf("required %s namespace missing", name)
		}
	}
	uid, err := mappedHost(o.Controls["uid_map"], 65537)
	if err != nil || uid != o.HostUID || uid < 11000 || uid == 65537 {
		return fmt.Errorf("leased host UID/map mismatch")
	}
	if _, err = mappedHost(o.Controls["gid_map"], 0); err != nil {
		return err
	}
	return nil
}
func RequireNativeResourceSample(s ResourceSample) error {
	if s.Error != "" || s.NativeSamples <= 0 || s.Samples < s.NativeSamples || s.NativeCapabilities != 0 || s.IntervalMicros <= 0 || s.PIDs <= 0 || s.FDs <= 0 || s.DiskBytes <= 0 {
		return fmt.Errorf("native resource evidence unavailable: %+v", s)
	}
	return requireNativeCustody(s.Custody)
}
func requireNativeCustody(c *NativeCustody) error {
	if c == nil || c.ObservedAt <= 0 || c.HostUID < 11000 || c.HostUID == 65537 || c.UserNS == "" || c.PIDNS == "" || c.Cgroup == "" || (c.Mode != "none" && c.Mode != "scoped") {
		return fmt.Errorf("missing bound live custody")
	}
	p := c.Payload
	g := c.GatedPayload
	if p.PID <= 1 || p.PID != p.TGID || p.NamespacePID != 1 || p.PID != g.PID || p.StartTime == "" || p.StartTime != g.StartTime {
		return fmt.Errorf("live payload identity replaced by helper or reused PID")
	}
	exe := filepath.Base(p.Executable)
	if !((c.Language == "python3" && (exe == "python3" || exe == "python3.12")) || (c.Language == "nodejs" && exe == "node")) {
		return fmt.Errorf("actual payload executable/language mismatch")
	}
	check := func(p ProcessIdentity) error {
		if p.PID <= 1 || p.TGID <= 1 || p.StartTime == "" || p.UserNS != c.UserNS || p.PIDNS != c.PIDNS || p.Cgroup != c.Cgroup {
			return fmt.Errorf("live process identity/namespace/cgroup mismatch")
		}
		for i := 0; i < 4; i++ {
			if p.UIDs[i] != c.HostUID || p.GIDs[i] != c.HostGID {
				return fmt.Errorf("live payload/thread UID/GID does not match leased mapping")
			}
		}
		if p.CapEff|p.CapPrm|p.CapAmb != 0 {
			return fmt.Errorf("live payload/thread retained capabilities")
		}
		return nil
	}
	if err := check(p); err != nil {
		return err
	}
	seen := map[int]bool{}
	for _, thread := range c.Threads {
		if seen[thread.PID] {
			return fmt.Errorf("duplicate live thread")
		}
		seen[thread.PID] = true
		if err := check(thread); err != nil {
			return err
		}
		if thread.PID == p.PID && thread != p {
			return fmt.Errorf("payload/thread identity conflict")
		}
	}
	if len(c.RequiredThreads) == 0 || len(c.RequiredThreads) != len(seen) || !seen[p.PID] {
		return fmt.Errorf("required live payload/thread was not inspected")
	}
	required := map[int]bool{}
	for _, id := range c.RequiredThreads {
		if required[id] || !seen[id] {
			return fmt.Errorf("required live thread evidence unavailable")
		}
		required[id] = true
	}
	return nil
}
func readNativeResource(directory, id, language, mode string) (ResourceSample, error) {
	var s ResourceSample
	raw, err := os.ReadFile(filepath.Join(directory, id+".resources.json"))
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	if err = RequireNativeResourceSample(s); err != nil {
		return s, err
	}
	if s.Custody.Language != language || s.Custody.Mode != mode {
		return s, fmt.Errorf("live custody does not match requested language/scope")
	}
	return s, nil
}
func (p procReader) path(id int, part string) string {
	return filepath.Join(p.root, strconv.Itoa(id), part)
}
func startTime(raw []byte) (string, error) {
	_, start, err := statIdentity(raw)
	return start, err
}
func statIdentity(raw []byte) (string, string, error) {
	end := strings.LastIndex(string(raw), ") ")
	if end < 0 {
		return "", "", fmt.Errorf("invalid proc stat")
	}
	fields := strings.Fields(string(raw)[end+2:])
	if len(fields) < 20 {
		return "", "", fmt.Errorf("short proc stat")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", "", err
	}
	return fields[0], fields[19], nil
}
func (p procReader) start(id int) (string, error) {
	raw, err := os.ReadFile(p.path(id, "stat"))
	if err != nil {
		return "", err
	}
	return startTime(raw)
}
func ids(path string) ([]int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := []int{}
	seen := map[int]bool{}
	for _, v := range strings.Fields(string(raw)) {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 1 || seen[n] {
			return nil, fmt.Errorf("invalid/duplicate owned task ID")
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}
func (p procReader) identity(id int) (ProcessIdentity, error) {
	v := ProcessIdentity{PID: id}
	var err error
	if v.StartTime, err = p.start(id); err != nil {
		return v, err
	}
	raw, err := os.ReadFile(p.path(id, "cgroup"))
	if err != nil {
		return v, err
	}
	v.Cgroup = strings.TrimSpace(string(raw))
	raw, err = os.ReadFile(p.path(id, "status"))
	if err != nil {
		return v, err
	}
	fields := map[string][]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		at := strings.IndexByte(line, ':')
		if at > 0 {
			fields[line[:at]] = strings.Fields(line[at+1:])
		}
	}
	for key, target := range map[string]*[4]int{"Uid": &v.UIDs, "Gid": &v.GIDs} {
		values := fields[key]
		if len(values) != 4 {
			return v, fmt.Errorf("missing %s", key)
		}
		for i, value := range values {
			n, err := strconv.Atoi(value)
			if err != nil {
				return v, err
			}
			target[i] = n
		}
	}
	if len(fields["Tgid"]) != 1 || len(fields["NSpid"]) < 2 {
		return v, fmt.Errorf("missing nested PID identity")
	}
	if v.TGID, err = strconv.Atoi(fields["Tgid"][0]); err != nil {
		return v, err
	}
	if v.NamespacePID, err = strconv.Atoi(fields["NSpid"][len(fields["NSpid"])-1]); err != nil {
		return v, err
	}
	for key, target := range map[string]*uint64{"CapEff": &v.CapEff, "CapPrm": &v.CapPrm, "CapAmb": &v.CapAmb} {
		if len(fields[key]) != 1 {
			return v, fmt.Errorf("missing %s", key)
		}
		*target, err = strconv.ParseUint(fields[key][0], 16, 64)
		if err != nil {
			return v, err
		}
	}
	if v.UserNS, err = os.Readlink(p.path(id, "ns/user")); err != nil {
		return v, err
	}
	if v.PIDNS, err = os.Readlink(p.path(id, "ns/pid")); err != nil {
		return v, err
	}
	if v.Executable, err = os.Readlink(p.path(id, "exe")); err != nil {
		return v, err
	}
	again, err := p.start(id)
	if err != nil {
		return v, fmt.Errorf("task exited while inspecting: %w", err)
	}
	if again != v.StartTime {
		return v, fmt.Errorf("task reused while inspecting")
	}
	raw, err = os.ReadFile(p.path(id, "cgroup"))
	if err != nil || strings.TrimSpace(string(raw)) != v.Cgroup {
		return v, fmt.Errorf("task membership changed while inspecting")
	}
	return v, nil
}

// An unavailable task is ignored only after BOTH kernel observations prove its
// original identity left the owned membership. Permission denial is not exit.
func (p procReader) gone(id int, start, cgroup string) bool {
	raw, err := os.ReadFile(p.path(id, "stat"))
	if err != nil && !os.IsNotExist(err) {
		return false
	}
	if err == nil {
		state, current, parseErr := statIdentity(raw)
		if parseErr != nil || start == "" {
			return false
		}
		// The supervisor holds the exited leader unreaped while monitors finish.
		// A matching zombie is terminal, but only absent active membership below
		// corroborates that state. It never supplies a fresh positive frame.
		if current == start && state != "Z" && state != "X" {
			return false
		}
	}
	members, err := ids(filepath.Join(cgroup, "cgroup.threads"))
	if err != nil {
		// Linux cannot remove a populated cgroup. Require the directory itself to
		// be gone as well as the original process, not just an unreadable file.
		if !os.IsNotExist(err) {
			return false
		}
		_, directoryErr := os.Stat(cgroup)
		return os.IsNotExist(directoryErr)
	}
	for _, member := range members {
		if member == id {
			return false
		}
	}
	return true
}
func (p procReader) gatedPayload(o isolation.Observation) (ProcessIdentity, error) {
	members, err := ids(filepath.Join(o.Cgroup, "cgroup.procs"))
	if err != nil {
		return ProcessIdentity{}, err
	}
	var payload ProcessIdentity
	for _, id := range members {
		v, err := p.identity(id)
		if err != nil {
			return payload, fmt.Errorf("gated owned process unavailable: %w", err)
		}
		if v.PIDNS != o.Namespaces["pid"] || v.UserNS != o.Namespaces["user"] || !strings.HasSuffix(v.Cgroup, "/"+filepath.Base(o.Cgroup)) {
			return payload, fmt.Errorf("gated process escaped owned namespace/cgroup")
		}
		if v.NamespacePID == 1 {
			if payload.PID != 0 {
				return payload, fmt.Errorf("multiple payload leaders")
			}
			payload = v
		}
	}
	if payload.PID <= 1 || payload.PID != payload.TGID {
		return payload, fmt.Errorf("actual gated payload PID1 absent")
	}
	return payload, nil
}
func (p procReader) capture(o isolation.Observation, gated ProcessIdentity, language, mode string) (*NativeCustody, int64, int64, int64, error) {
	members, err := ids(filepath.Join(o.Cgroup, "cgroup.threads"))
	if err != nil {
		if p.gone(gated.PID, gated.StartTime, o.Cgroup) {
			return nil, 0, 0, 0, nil
		}
		return nil, 0, 0, 0, err
	}
	gid, err := mappedHost(o.Controls["gid_map"], 0)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	c := &NativeCustody{Language: language, Mode: mode, HostUID: o.HostUID, HostGID: gid, UserNS: o.Namespaces["user"], PIDNS: o.Namespaces["pid"], Cgroup: gated.Cgroup, GatedPayload: gated, ObservedAt: time.Now().UnixNano()}
	processes := map[int]bool{}
	var fds, disk int64
	for _, id := range members {
		v, err := p.identity(id)
		if err != nil {
			if p.gone(id, v.StartTime, o.Cgroup) {
				continue
			}
			return nil, 0, 0, 0, fmt.Errorf("still-live owned thread %d unavailable: %w", id, err)
		}
		c.RequiredThreads = append(c.RequiredThreads, id)
		c.Threads = append(c.Threads, v)
		if id == gated.PID {
			c.Payload = v
		}
		if processes[v.TGID] {
			continue
		}
		processes[v.TGID] = true
		files, err := os.ReadDir(p.path(v.TGID, "fd"))
		if err != nil {
			if p.gone(id, v.StartTime, o.Cgroup) {
				return nil, 0, 0, 0, nil
			}
			return nil, 0, 0, 0, fmt.Errorf("live owned process FD access unavailable: %w", err)
		}
		var stat syscall.Statfs_t
		if err = syscall.Statfs(p.path(v.TGID, "root/work"), &stat); err != nil {
			if p.gone(id, v.StartTime, o.Cgroup) {
				return nil, 0, 0, 0, nil
			}
			return nil, 0, 0, 0, fmt.Errorf("live owned process root access unavailable: %w", err)
		}
		if stat.Type != 0x01021994 {
			return nil, 0, 0, 0, fmt.Errorf("owned work is not actual tmpfs")
		}
		fds += int64(len(files))
		used := (int64(stat.Blocks) - int64(stat.Bfree)) * stat.Bsize
		disk = max64(disk, used)
	}
	if c.Payload.PID == 0 {
		if p.gone(gated.PID, gated.StartTime, o.Cgroup) {
			return nil, 0, 0, 0, nil
		}
		return nil, 0, 0, 0, fmt.Errorf("live payload omitted from owned thread evidence")
	}
	// A new live thread cannot be silently omitted by a concurrent membership change.
	after, err := ids(filepath.Join(o.Cgroup, "cgroup.threads"))
	if err != nil {
		if p.gone(gated.PID, gated.StartTime, o.Cgroup) {
			return nil, 0, 0, 0, nil
		}
		return nil, 0, 0, 0, err
	}
	seen := map[int]bool{}
	for _, id := range c.RequiredThreads {
		seen[id] = true
	}
	for _, id := range after {
		if !seen[id] {
			return nil, 0, 0, 0, fmt.Errorf("new owned thread not inspected in stable snapshot")
		}
	}
	if err = requireNativeCustody(c); err != nil {
		return nil, 0, 0, 0, err
	}
	raw, err := os.ReadFile(filepath.Join(o.Cgroup, "pids.current"))
	if err != nil {
		if p.gone(gated.PID, gated.StartTime, o.Cgroup) {
			return nil, 0, 0, 0, nil
		}
		return nil, 0, 0, 0, err
	}
	pids, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || pids <= 0 {
		if p.gone(gated.PID, gated.StartTime, o.Cgroup) {
			return nil, 0, 0, 0, nil
		}
		return nil, 0, 0, 0, fmt.Errorf("invalid actual PID count")
	}
	return c, fds, disk, pids, nil
}
