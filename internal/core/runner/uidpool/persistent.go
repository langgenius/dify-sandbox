package uidpool

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// Ordinary leases are private service metadata, outside both native roots.
// They contain no submitted code, results, credentials or process identifiers.
const ordinaryLeaseRoot = "/var/sandbox/ordinary-leases"

type leaseRecord struct {
	UID       int      `json:"uid"`
	Resources []string `json:"resources"`
}
type ownedLease struct {
	file   *os.File
	record leaseRecord
}
type persistentPool struct {
	mu    sync.Mutex
	root  string
	owned map[int]*ownedLease
}

// NewPersistentUIDPool reconciles each candidate at acquisition under its own
// cross-process lock. The lock spans the birth window, launch and cleanup, and
// is released by the kernel on server death. Recovery never signals a PID or
// removes a recorded resource: both process and resource absence are required.
func NewPersistentUIDPool(min, max int, root string) (*UIDPool, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, fmt.Errorf("invalid lease root")
	}
	parent := filepath.Dir(root)
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		return nil, fmt.Errorf("invalid lease root parent: %v", err)
	}
	// Ancestors must not allow sandbox UIDs to replace the private journal.
	for p := parent; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) || !info.IsDir() || (info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0) {
			return nil, fmt.Errorf("untrusted lease root parent")
		}
		if p == "/" {
			break
		}
	}
	if err := os.Mkdir(root, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	if err := privateLeasePath(root, true); err != nil {
		return nil, err
	}
	if err := syncDirectory(parent); err != nil {
		return nil, err
	}
	p := NewUIDPool(min, max)
	p.persistent = &persistentPool{root: root, owned: make(map[int]*ownedLease)}
	return p, nil
}
func privateLeasePath(path string, dir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0077 != 0 || info.Mode()&os.ModeSymlink != 0 || info.IsDir() != dir || (!dir && !info.Mode().IsRegular()) {
		return fmt.Errorf("lease metadata must be private and service-owned")
	}
	return nil
}
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func (p *persistentPool) recordPath(uid int) string {
	return filepath.Join(p.root, strconv.Itoa(uid)+".json")
}
func (p *persistentPool) save(record leaseRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(p.root, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
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
	if err = os.Rename(f.Name(), p.recordPath(record.UID)); err != nil {
		return err
	}
	return syncDirectory(p.root)
}
func (p *persistentPool) clean(record leaseRecord) error {
	if err := VerifyUIDIdle(record.UID); err != nil {
		return err
	}
	for _, resource := range record.Resources {
		if !filepath.IsAbs(resource) || filepath.Clean(resource) != resource || resource == p.root || strings.HasPrefix(resource, p.root+"/") {
			return fmt.Errorf("invalid recorded resource")
		}
		if _, err := os.Lstat(resource); !os.IsNotExist(err) {
			return fmt.Errorf("resource not proven absent: %s (%v)", resource, err)
		}
	}
	return nil
}
func (p *persistentPool) acquire(uid int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	lockPath := filepath.Join(p.root, strconv.Itoa(uid)+".lock")
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), lockPath)
	owned := false
	defer func() {
		if !owned {
			f.Close()
		}
	}()
	if err := privateLeasePath(lockPath, false); err != nil {
		return err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return err
	}
	record := leaseRecord{UID: uid}
	recordPath := p.recordPath(uid)
	if err := privateLeasePath(recordPath, false); err == nil {
		recordFile, err := os.Open(recordPath)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(recordFile, 16385))
		recordFile.Close()
		if err != nil {
			return err
		}
		if len(data) > 16384 {
			return fmt.Errorf("oversized lease metadata")
		}
		record = leaseRecord{}
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("invalid lease metadata suffix")
		}
		if record.UID != uid {
			return fmt.Errorf("lease identity mismatch")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := p.clean(record); err != nil {
		return err
	}
	record.Resources = nil
	if err := p.save(record); err != nil {
		return err
	}
	p.owned[uid] = &ownedLease{file: f, record: record}
	owned = true
	return nil
}

// TrackResource records a service-generated path before anything is created at
// it. Callers must abort creation on error. Submitted paths never reach here.
func (p *UIDPool) TrackResource(uid int, resource string) error {
	if p.persistent == nil {
		return fmt.Errorf("persistent lease required")
	}
	j := p.persistent
	j.mu.Lock()
	defer j.mu.Unlock()
	lease, ok := j.owned[uid]
	if !ok {
		return fmt.Errorf("UID is not owned")
	}
	if !filepath.IsAbs(resource) || filepath.Clean(resource) != resource || resource == j.root || strings.HasPrefix(resource, j.root+"/") {
		return fmt.Errorf("invalid resource path")
	}
	lease.record.Resources = append(lease.record.Resources, resource)
	return j.save(lease.record)
}
func (p *persistentPool) release(uid int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	lease, ok := p.owned[uid]
	if !ok {
		return fmt.Errorf("UID is not owned")
	}
	if err := p.clean(lease.record); err != nil {
		return err
	}
	if err := os.Remove(p.recordPath(uid)); err != nil {
		return err
	}
	if err := syncDirectory(p.root); err != nil {
		return err
	}
	if err := lease.file.Close(); err != nil {
		return err
	}
	delete(p.owned, uid)
	return nil
}
func TrackResource(uid int, resource string) error {
	if globalPool == nil {
		return fmt.Errorf("UID pool is not initialized")
	}
	return globalPool.TrackResource(uid, resource)
}
