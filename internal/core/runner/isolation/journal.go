package isolation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Journal records ownership, never payloads, output, credentials or launch env.
// Unknown recovered outcomes are tombstones, not permission to dispatch again.
type journalRecord struct {
	ID            string `json:"execution_id"`
	Digest        string `json:"invocation_digest"`
	Root          string `json:"root"`
	Cgroup        string `json:"cgroup"`
	State         string `json:"state"`
	PID           int    `json:"pid,omitempty"`
	StartTime     string `json:"start_time,omitempty"`
	CleanupMillis int64  `json:"cleanup_millis"`
}
type journal struct {
	root string
	lock *os.File
}

func privatePath(path string, dir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode()&os.ModeSymlink != 0 || info.IsDir() != dir || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("journal requires private service-owned paths")
	}
	if !dir && !info.Mode().IsRegular() {
		return fmt.Errorf("journal requires regular file")
	}
	return nil
}
func openJournal(p Profile) (*journal, error) {
	root := p.JournalRoot
	if root == "" {
		root = p.WorkRoot + ".journal"
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, fmt.Errorf("invalid journal root")
	}
	// Resolve only parents: the journal itself must not be a symlink.
	parent, err := filepath.EvalSymlinks(filepath.Dir(root))
	if err != nil {
		return nil, err
	}
	root = filepath.Join(parent, filepath.Base(root))
	for _, child := range []string{p.EnvironmentRoot, p.WorkRoot, p.CgroupRoot} {
		if root == child || strings.HasPrefix(root, child+"/") || strings.HasPrefix(child, root+"/") {
			return nil, fmt.Errorf("journal overlaps execution roots")
		}
	}
	if err = os.Mkdir(root, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	if err = privatePath(root, true); err != nil {
		return nil, err
	}
	parentDir, err := os.Open(parent)
	if err != nil {
		return nil, err
	}
	err = parentDir.Sync()
	parentDir.Close()
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(root, "lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "journal-lock")
	if err = privatePath(filepath.Join(root, "lock"), false); err == nil {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return &journal{root: root, lock: f}, nil
}
func (j *journal) close() error {
	if j == nil || j.lock == nil {
		return nil
	}
	return j.lock.Close()
}
func journalName(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:]) + ".json"
}
func (j *journal) save(r journalRecord) error {
	if j == nil {
		return nil
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(j.root, ".pending-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(j.root, journalName(r.ID))); err != nil {
		return err
	}
	d, err := os.Open(j.root)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (e *Execution) saveJournal(state string) error {
	return e.journal.save(journalRecord{ID: e.spec.ExecutionID, Digest: e.spec.InvocationDigest, Root: e.WorkDir, Cgroup: e.cgroup, State: state, PID: e.ownedPID, StartTime: e.ownedStart, CleanupMillis: e.spec.Budget.CleanupMillis})
}
func (s *Supervisor) reconcile() error {
	entries, err := os.ReadDir(s.journal.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == "lock" {
			continue
		}
		path := filepath.Join(s.journal.root, entry.Name())
		if err = privatePath(path, false); err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".pending-") {
			if err = os.Remove(path); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unrecognized journal entry")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(f, 8193))
		f.Close()
		if err != nil {
			return err
		}
		if len(data) > 8192 {
			return fmt.Errorf("oversized journal")
		}
		var r journalRecord
		if err = decodeStrict(data, &r); err != nil {
			return err
		}
		if !idPattern.MatchString(r.ID) || !digestPattern.MatchString(r.Digest) || journalName(r.ID) != entry.Name() || r.PID < 0 || (r.PID > 0 && r.StartTime == "") || r.CleanupMillis <= 0 || r.CleanupMillis > s.profile.Limits.CleanupMillis {
			return fmt.Errorf("invalid journal identity")
		}
		if (r.Root != "" && (filepath.Dir(r.Root) != s.profile.WorkRoot || !strings.HasPrefix(filepath.Base(r.Root), "request-"))) || (r.Cgroup != "" && (r.Root == "" || r.Cgroup != filepath.Join(s.profile.CgroupRoot, filepath.Base(r.Root)))) {
			return fmt.Errorf("journal resource outside owned roots")
		}
		e := &Execution{spec: InvocationSpec{ExecutionID: r.ID, InvocationDigest: r.Digest, Budget: Budget{CleanupMillis: r.CleanupMillis}}, profile: s.profile, ownedPID: r.PID, ownedStart: r.StartTime, WorkDir: r.Root, cgroup: r.Cgroup, journal: s.journal, done: make(chan struct{}), origin: s.origin, result: ExecutionResult{ExecutionID: r.ID, InvocationDigest: r.Digest, Status: "unknown", FailureReason: "result_unavailable", CleanupState: "clean", ExitCode: -1}}
		if err = e.cleanup(); err != nil {
			e.result.CleanupState = "quarantined"
			e.result.FailureReason = "cleanup_failed"
			s.admission.running++
			s.admission.quarantined++
		}
		if e.result.CleanupState == "quarantined" {
			e.state = "quarantined"
		} else {
			e.state = "terminal"
		}
		if err = e.saveJournal(e.state); err != nil {
			return err
		}
		close(e.done)
		if _, _, err = s.registry.register(r.ID, r.Digest, e); err != nil {
			return err
		}
	}
	// Unknown roots may predate a torn first record. Never reuse capacity while
	// unowned resources exist: fail startup instead of guessing their identity.
	roots, err := os.ReadDir(s.profile.WorkRoot)
	if err != nil {
		return err
	}
	for _, r := range roots {
		owned := false
		for _, v := range s.registry.entries {
			owned = owned || v.execution.WorkDir == filepath.Join(s.profile.WorkRoot, r.Name())
		}
		if !owned {
			return fmt.Errorf("unowned execution root requires operator reconciliation")
		}
	}
	groups, err := os.ReadDir(s.profile.CgroupRoot)
	if err != nil {
		return err
	}
	for _, g := range groups {
		if !g.IsDir() && g.Type()&os.ModeSymlink == 0 {
			continue
		}
		owned := false
		for _, v := range s.registry.entries {
			owned = owned || v.execution.cgroup == filepath.Join(s.profile.CgroupRoot, g.Name())
		}
		if !owned {
			return fmt.Errorf("unowned cgroup requires operator reconciliation")
		}
	}
	return nil
}
