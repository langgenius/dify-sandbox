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
	UID           int      `json:"uid"`
	Resources     []string `json:"resources"`
	Custody       string   `json:"custody,omitempty"`
	CustodyDigest string   `json:"custody_digest,omitempty"`
	Released      bool     `json:"released,omitempty"`
}
type ownedLease struct {
	file      *os.File
	record    leaseRecord
	recovered bool
}
type persistentPool struct {
	mu     sync.Mutex
	root   string
	owned  map[int]*ownedLease
	closed bool
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
		if !ok || (stat.Uid != 0 && stat.Uid != uint32(os.Geteuid())) || !info.IsDir() || (info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0) {
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

// Caller holds mu. Opening a retained native lease does not grant permission
// to reuse its UID; only the owning execution journal can reconcile custody.
func (p *persistentPool) lockRecord(uid int) (*ownedLease, error) {
	if p.closed {
		return nil, fmt.Errorf("UID pool closed")
	}
	lockPath := filepath.Join(p.root, strconv.Itoa(uid)+".lock")
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), lockPath)
	owned := false
	defer func() {
		if !owned {
			f.Close()
		}
	}()
	if err := privateLeasePath(lockPath, false); err != nil {
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, err
	}
	record, err := p.readRecord(uid)
	if err != nil {
		return nil, err
	}
	owned = true
	return &ownedLease{file: f, record: record}, nil
}
func (p *persistentPool) readRecord(uid int) (leaseRecord, error) {
	record := leaseRecord{UID: uid}
	recordPath := p.recordPath(uid)
	if err := privateLeasePath(recordPath, false); err == nil {
		recordFile, err := os.Open(recordPath)
		if err != nil {
			return record, err
		}
		data, err := io.ReadAll(io.LimitReader(recordFile, 16385))
		recordFile.Close()
		if err != nil {
			return record, err
		}
		if len(data) > 16384 {
			return record, fmt.Errorf("oversized lease metadata")
		}
		record = leaseRecord{}
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return record, err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return record, fmt.Errorf("invalid lease metadata suffix")
		}
		if record.UID != uid {
			return record, fmt.Errorf("lease identity mismatch")
		}
	} else if !os.IsNotExist(err) {
		return record, err
	}
	linked := false
	for _, resource := range record.Resources {
		if !filepath.IsAbs(resource) || filepath.Clean(resource) != resource || resource == p.root || strings.HasPrefix(resource, p.root+"/") {
			return record, fmt.Errorf("invalid lease resource")
		}
		linked = linked || resource == record.Custody
	}
	if record.Custody != "" {
		if !linked || len(record.CustodyDigest) != 64 || strings.Trim(record.CustodyDigest, "0123456789abcdef") != "" {
			return record, fmt.Errorf("invalid native custody binding")
		}
	} else if record.CustodyDigest != "" || record.Released {
		return record, fmt.Errorf("native custody identity missing")
	}
	return record, nil
}

// ValidateCustodyOwners is a bounded read-only startup audit. A crash between
// durable lease binding and the first marker/journal update must fail closed,
// rather than silently abandoning an unattributed UID in the finite pool.
func (p *UIDPool) ValidateCustodyOwners(owns func(int, string, string, bool) bool) error {
	if p.persistent == nil || owns == nil {
		return fmt.Errorf("native custody owner required")
	}
	j := p.persistent
	j.mu.Lock()
	defer j.mu.Unlock()
	for uid := p.min; uid < p.max; uid++ {
		record, err := j.readRecord(uid)
		if err != nil {
			return err
		}
		if record.Custody != "" && !owns(uid, record.Custody, record.CustodyDigest, record.Released) {
			return fmt.Errorf("native lease without matching execution journal")
		}
	}
	return nil
}
func (p *persistentPool) acquire(uid int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	lease, err := p.lockRecord(uid)
	if err != nil {
		return err
	}
	owned := false
	defer func() {
		if !owned {
			lease.file.Close()
		}
	}()
	if lease.record.Custody != "" && !lease.record.Released {
		return fmt.Errorf("native UID requires journal reconciliation")
	}
	if err := p.clean(lease.record); err != nil {
		return err
	}
	lease.record.Resources = nil
	lease.record.Custody = ""
	lease.record.CustodyDigest = ""
	lease.record.Released = false
	if err := p.save(lease.record); err != nil {
		return err
	}
	p.owned[uid] = lease
	owned = true
	return nil
}

// TrackCustody binds an existing native lease to its execution's private
// pending-ack path. Unlike an ordinary resource, an absent marker alone never
// proves that an interrupted native release has completed.
func (p *UIDPool) TrackCustody(uid int, resource, digest string) error {
	if len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		return fmt.Errorf("invalid custody digest")
	}
	if p.persistent == nil {
		return fmt.Errorf("persistent native custody required")
	}
	j := p.persistent
	j.mu.Lock()
	defer j.mu.Unlock()
	if !filepath.IsAbs(resource) || filepath.Clean(resource) != resource || resource == j.root || strings.HasPrefix(resource, j.root+"/") {
		return fmt.Errorf("invalid custody resource path")
	}
	lease, ok := j.owned[uid]
	if !ok || (lease.record.Custody != "" && (lease.record.Custody != resource || lease.record.CustodyDigest != digest)) {
		return fmt.Errorf("native custody identity mismatch")
	}
	linked := false
	for _, path := range lease.record.Resources {
		linked = linked || path == resource
	}
	if !linked {
		lease.record.Resources = append(lease.record.Resources, resource)
	}
	lease.record.Custody = resource
	lease.record.CustodyDigest = digest
	lease.record.Released = false
	return j.save(lease.record)
}

// RecoverCustody adopts the same persisted lease before supervisor admission.
// It does not allocate a new identity. Call only while this pool has no callers.
// A false return means that this historical execution no longer owns the UID.
func (p *UIDPool) RecoverCustody(uid int, resource, digest string, retained bool) (bool, error) {
	if p.persistent == nil || uid < p.min || uid >= p.max {
		return false, fmt.Errorf("invalid native custody pool")
	}
	j := p.persistent
	j.mu.Lock()
	defer j.mu.Unlock()
	if lease, ok := j.owned[uid]; ok {
		return lease.record.Custody == resource && lease.record.CustodyDigest == digest, nil
	}
	lease, err := j.lockRecord(uid)
	if err != nil {
		return false, err
	}
	if lease.record.Custody == "" {
		lease.file.Close()
		return false, fmt.Errorf("execution lease custody missing")
	}
	if lease.record.Custody != resource || lease.record.CustodyDigest != digest || (lease.record.Released && !retained) {
		lease.file.Close()
		return false, nil
	}
	j.owned[uid] = lease
	lease.recovered = true
	return true, nil
}

// A recovered lease still has its original candidate in the fresh pool queue.
// Return it only once. Quarantined candidates fail their held flock check when
// acquired, leaving other UIDs available without changing acquisition order.
func (p *UIDPool) ReleaseCustody(uid int) error {
	if p.persistent == nil || uid < p.min || uid >= p.max {
		return fmt.Errorf("invalid native custody pool")
	}
	j := p.persistent
	j.mu.Lock()
	lease, ok := j.owned[uid]
	if !ok {
		j.mu.Unlock()
		return fmt.Errorf("UID is not owned")
	}
	recovered := lease.recovered
	j.mu.Unlock()
	if err := j.release(uid); err != nil {
		return err
	}
	if !recovered {
		p.pool <- uid
	}
	return nil
}

// CheckCustody proves process and all other resource absence while retaining
// the pending marker and flock. Final ReleaseChecked repeats the whole proof.
func (p *UIDPool) CheckCustody(uid int, resource string) error {
	if p.persistent == nil {
		return fmt.Errorf("persistent native custody required")
	}
	j := p.persistent
	j.mu.Lock()
	defer j.mu.Unlock()
	lease, ok := j.owned[uid]
	if !ok || lease.record.Custody != resource {
		return fmt.Errorf("native custody is not owned")
	}
	record := lease.record
	record.Resources = nil
	for _, path := range lease.record.Resources {
		if path != resource {
			record.Resources = append(record.Resources, path)
		}
	}
	return j.clean(record)
}

// Close relinquishes only process-local handles. Durable records and resources
// stay intact for the next supervisor; no finalizer is part of the handoff.
func (p *UIDPool) Close() error {
	if p == nil || p.persistent == nil {
		return nil
	}
	j := p.persistent
	j.mu.Lock()
	defer j.mu.Unlock()
	j.closed = true
	var first error
	for uid, lease := range j.owned {
		if err := lease.file.Close(); err != nil && first == nil {
			first = err
		}
		delete(j.owned, uid)
	}
	return first
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
	if lease.record.Custody != "" {
		// Keep the identity even through a failed final metadata fsync. A
		// native tombstone becomes reusable only after the complete absence
		// proof above; unfinished custody is never inferred from a missing file.
		released := lease.record
		released.Released = true
		if err := p.save(released); err != nil {
			return err
		}
	} else {
		if err := os.Remove(p.recordPath(uid)); err != nil {
			return err
		}
		if err := syncDirectory(p.root); err != nil {
			return err
		}
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
