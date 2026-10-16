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
	ID                string `json:"execution_id"`
	Digest            string `json:"invocation_digest"`
	Root              string `json:"root"`
	Cgroup            string `json:"cgroup"`
	State             string `json:"state"`
	PID               int    `json:"pid,omitempty"`
	StartTime         string `json:"start_time,omitempty"`
	CleanupMillis     int64  `json:"cleanup_millis"`
	HostUID           int    `json:"host_uid,omitempty"`
	UIDReleasePending bool   `json:"uid_release_pending,omitempty"`
}
type uidCustodyRecord struct {
	ID     string `json:"execution_id"`
	Digest string `json:"invocation_digest"`
	UID    int    `json:"host_uid"`
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
func custodyName(id string) string {
	return strings.TrimSuffix(journalName(id), ".json") + ".uid-pending"
}
func syncJournalDirectory(root string) error {
	d, err := os.Open(root)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// The pool's existing resource check must retain the lease across the gap
// between physical cleanup and the durable terminal acknowledgement.
func (e *Execution) recordUIDCustody() error {
	if e.hostUID == 0 || e.custodyRecorded {
		return nil
	}
	if e.journal == nil || e.uidPool == nil {
		return fmt.Errorf("UID custody requires durable execution ownership")
	}
	path := filepath.Join(e.journal.root, custodyName(e.spec.ExecutionID))
	if err := e.uidPool.TrackCustody(e.hostUID, path, e.spec.InvocationDigest); err != nil {
		return err
	}
	data, err := json.Marshal(uidCustodyRecord{ID: e.spec.ExecutionID, Digest: e.spec.InvocationDigest, UID: e.hostUID})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = syncJournalDirectory(e.journal.root)
	}
	if err == nil {
		e.custodyRecorded = true
	}
	return err
}

// Only callers that have completed cleanup and synced a terminal journal may
// remove this resource. Recovered executions never acquire a replacement UID.
func (e *Execution) clearUIDCustody() error {
	if e.hostUID == 0 || !e.custodyRecorded {
		return nil
	}
	if err := os.Remove(filepath.Join(e.journal.root, custodyName(e.spec.ExecutionID))); err != nil {
		return err
	}
	// Unlink already changed the filesystem even if the directory fsync fails.
	// Recovery can restore this marker from the still-retained lease binding.
	e.custodyRecorded = false
	return syncJournalDirectory(e.journal.root)
}
func (e *Execution) releaseUIDCustody() error {
	path := filepath.Join(e.journal.root, custodyName(e.spec.ExecutionID))
	if err := e.uidPool.CheckCustody(e.hostUID, path); err != nil {
		return err
	}
	if err := e.clearUIDCustody(); err != nil {
		return err
	}
	return e.uidPool.ReleaseCustody(e.hostUID)
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
	if state == "terminal" && e.hostUID != 0 {
		e.uidReleasePending = true
	}
	if err := e.recordUIDCustody(); err != nil {
		return err
	}
	return e.journal.save(journalRecord{ID: e.spec.ExecutionID, Digest: e.spec.InvocationDigest, Root: e.WorkDir, Cgroup: e.cgroup, State: state, PID: e.ownedPID, StartTime: e.ownedStart, CleanupMillis: e.spec.Budget.CleanupMillis, HostUID: e.hostUID, UIDReleasePending: e.uidReleasePending})
}
func (s *Supervisor) reconcile() error {
	entries, err := os.ReadDir(s.journal.root)
	if err != nil {
		return err
	}
	// Validate all pending custody links before cleanup can remove any evidence.
	custody := make(map[string]uidCustodyRecord)
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".uid-pending") {
			continue
		}
		path := filepath.Join(s.journal.root, entry.Name())
		if err := privatePath(path, false); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(f, 1025))
		f.Close()
		if err != nil || len(data) > 1024 {
			return fmt.Errorf("invalid UID custody record: %v", err)
		}
		var r uidCustodyRecord
		if err := decodeStrict(data, &r); err != nil {
			return err
		}
		if !idPattern.MatchString(r.ID) || !digestPattern.MatchString(r.Digest) || custodyName(r.ID) != entry.Name() || r.UID < s.profile.HostUIDMin || r.UID >= s.profile.HostUIDMax {
			return fmt.Errorf("invalid UID custody identity")
		}
		if err := privatePath(filepath.Join(s.journal.root, journalName(r.ID)), false); err != nil {
			return fmt.Errorf("UID custody without execution journal: %w", err)
		}
		custody[r.ID] = r
	}
	for _, entry := range entries {
		if entry.Name() == "lock" || strings.HasSuffix(entry.Name(), ".uid-pending") {
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
		pending, hasCustody := custody[r.ID]
		if r.HostUID != 0 && (r.HostUID < s.profile.HostUIDMin || r.HostUID >= s.profile.HostUIDMax) {
			return fmt.Errorf("execution UID outside native pool")
		}
		if (hasCustody && (pending.UID != r.HostUID || pending.Digest != r.Digest)) || (r.HostUID != 0 && r.State != "terminal" && !hasCustody && !r.UIDReleasePending) {
			return fmt.Errorf("execution UID custody mismatch")
		}
		if r.State != "queued" && r.State != "running" && r.State != "terminal" && r.State != "quarantined" {
			return fmt.Errorf("invalid execution state")
		}
		owned := false
		if r.HostUID != 0 {
			owned, err = s.uidPool.RecoverCustody(r.HostUID, filepath.Join(s.journal.root, custodyName(r.ID)), r.Digest, hasCustody || r.State != "terminal")
			if err != nil {
				return err
			}
			if (hasCustody && !owned) || (!hasCustody && owned && !r.UIDReleasePending) || (r.State != "terminal" && !owned) {
				return fmt.Errorf("execution lease custody mismatch")
			}
		}
		e := &Execution{spec: InvocationSpec{ExecutionID: r.ID, InvocationDigest: r.Digest, Budget: Budget{CleanupMillis: r.CleanupMillis}}, profile: s.profile, ownedPID: r.PID, ownedStart: r.StartTime, WorkDir: r.Root, cgroup: r.Cgroup, journal: s.journal, hostUID: r.HostUID, custodyRecorded: hasCustody, uidReleasePending: r.UIDReleasePending, done: make(chan struct{}), origin: s.origin, result: ExecutionResult{ExecutionID: r.ID, InvocationDigest: r.Digest, Status: "unknown", FailureReason: "result_unavailable", CleanupState: "clean", ExitCode: -1}}
		if owned {
			e.uidPool = s.uidPool
			if err := e.recordUIDCustody(); err != nil {
				return err
			}
		}
		err = e.cleanup()
		if err == nil && owned {
			err = s.uidPool.CheckCustody(r.HostUID, filepath.Join(s.journal.root, custodyName(r.ID)))
		}
		if err != nil {
			e.result.CleanupState = "quarantined"
			e.result.FailureReason = "cleanup_failed"
		}
		if e.result.CleanupState == "quarantined" {
			e.state = "quarantined"
		} else {
			e.state = "terminal"
		}
		r.State = e.state
		if owned {
			err = e.saveJournal(e.state)
		} else {
			err = s.journal.save(r)
		}
		if err != nil {
			return err
		}
		if e.state == "terminal" && owned {
			if err := e.releaseUIDCustody(); err != nil {
				e.state = "quarantined"
				e.result.CleanupState = "quarantined"
				e.result.FailureReason = "cleanup_failed"
				if err := e.saveJournal(e.state); err != nil {
					return err
				}
			}
		}
		if e.state == "quarantined" {
			s.admission.running++
			s.admission.quarantined++
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
	return s.uidPool.ValidateCustodyOwners(func(uid int, path, digest string, released bool) bool {
		for _, v := range s.registry.entries {
			e := v.execution
			if (released || e.uidPool == s.uidPool) && e.hostUID == uid && e.spec.InvocationDigest == digest && path == filepath.Join(s.journal.root, custodyName(e.spec.ExecutionID)) {
				return true
			}
		}
		return false
	})
}
