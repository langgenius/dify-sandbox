package cases

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"golang.org/x/sys/unix"
)

const hostSentinelContent = "CS5 synthetic host sentinel\n"
const controlSentinelContent = "CS5 synthetic control sentinel\n"

func privateSentinelValue(id string) string { return "CS5 private " + id }

type SentinelObservation struct {
	HostPath, ControlPath, PrivateDigest, HostDigest, ControlDigest string
	SignalSentAt                                                    int64
	SignalCount                                                     int
}
type ReadAttempt struct {
	Path  string `json:"path"`
	Errno int    `json:"errno"`
}
type PeerAttack struct {
	Reads        []ReadAttempt `json:"reads"`
	SignalPID    int           `json:"signal_pid"`
	SignalTID    int           `json:"signal_tid"`
	SignalStart  string        `json:"signal_start"`
	SignalUserNS string        `json:"signal_userns"`
	SignalPIDNS  string        `json:"signal_pidns"`
	SignalErrno  int           `json:"signal_errno"`
	SelfRead     string        `json:"self_read"`
	SelfSignals  int           `json:"self_signals"`
}
type PeerIsolationProof struct {
	Before, After, Attacker                  ResourceSample
	Attempt                                  PeerAttack
	BeganAt, CompletedAt                     int64
	Paths                                    []string
	PrivateDigest, HostDigest, ControlDigest string
}

func requirePeerIsolation(p PeerIsolationProof) error {
	for _, s := range []ResourceSample{p.Before, p.After, p.Attacker} {
		if err := RequireNativeResourceSample(s); err != nil {
			return err
		}
	}
	before, after, attacker := p.Before.Custody, p.After.Custody, p.Attacker.Custody
	if before.Payload.PID != after.Payload.PID || before.Payload.StartTime != after.Payload.StartTime || before.HostUID != after.HostUID || before.UserNS != after.UserNS || before.HostUID == attacker.HostUID || before.UserNS == attacker.UserNS || before.Mode != attacker.Mode {
		return fmt.Errorf("victim identity changed or custody overlaps attacker")
	}
	if !(before.ObservedAt < p.BeganAt && p.BeganAt < attacker.ObservedAt && attacker.ObservedAt <= p.CompletedAt && p.CompletedAt < after.ObservedAt) {
		return fmt.Errorf("independent live victim evidence does not span attack")
	}
	a, b := p.Before.Sentinel, p.After.Sentinel
	if a == nil || b == nil || a.SignalSentAt <= 0 || a.SignalSentAt >= before.ObservedAt || a.SignalCount != 1 || b.SignalCount != 1 || b.SignalSentAt != a.SignalSentAt {
		return fmt.Errorf("positive victim signal absent or attack changed signal count")
	}
	if p.PrivateDigest == "" || p.HostDigest == "" || p.ControlDigest == "" || a.PrivateDigest != p.PrivateDigest || b.PrivateDigest != p.PrivateDigest || a.HostDigest != p.HostDigest || b.HostDigest != p.HostDigest || a.ControlDigest != p.ControlDigest || b.ControlDigest != p.ControlDigest || a.HostPath != b.HostPath || a.ControlPath != b.ControlPath {
		return fmt.Errorf("synthetic sentinel positive read absent or contents changed")
	}
	if len(p.Paths) != 4 || len(p.Attempt.Reads) != len(p.Paths) || p.Paths[2] != a.HostPath || p.Paths[3] != a.ControlPath {
		return fmt.Errorf("complete peer/host/control path inventory required")
	}
	for i, attempt := range p.Attempt.Reads {
		if attempt.Path != p.Paths[i] || (attempt.Errno != int(unix.ENOENT) && attempt.Errno != int(unix.EACCES) && attempt.Errno != int(unix.EPERM)) {
			return fmt.Errorf("peer/host read succeeded or probe unavailable: %+v", attempt)
		}
	}
	if p.Attempt.SignalPID != before.Payload.PID || p.Attempt.SignalPID <= 1 || (p.Attempt.SignalErrno != int(unix.ESRCH) && p.Attempt.SignalErrno != int(unix.EPERM)) {
		return fmt.Errorf("peer signal succeeded, wrong target or unavailable probe")
	}
	if p.Attempt.SignalTID != before.Payload.PID || p.Attempt.SignalStart != before.Payload.StartTime || p.Attempt.SignalUserNS != before.UserNS || p.Attempt.SignalPIDNS != before.PIDNS {
		return fmt.Errorf("peer tgkill thread/start/namespace target mismatch")
	}
	if p.Attempt.SelfRead != "CS5 self-positive" || p.Attempt.SelfSignals != 1 {
		return fmt.Errorf("permitted payload read/signal control not exercised")
	}
	return nil
}
func readSynthetic(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "synthetic-sentinel")
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 256 {
		return nil, fmt.Errorf("synthetic sentinel must be bounded regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 257))
	if err != nil || len(raw) > 256 {
		return nil, fmt.Errorf("invalid bounded sentinel read: %v", err)
	}
	return raw, nil
}
func ensureSentinel(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0400)
	if err == nil {
		_, err = f.WriteString(content)
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	} else if os.IsExist(err) {
		err = nil
	}
	if err != nil {
		return err
	}
	raw, err := readSynthetic(path)
	if err != nil {
		return err
	}
	if string(raw) != content {
		return fmt.Errorf("synthetic host/control sentinel mismatch")
	}
	return nil
}
func (r *ResourceRecorder) observeSentinels(id string, e *resourceEntry) error {
	e.sample.Sentinel = nil
	c := e.sample.Custody
	if id != "cs1-custody-victim-"+e.mode+"-"+e.language || filepath.Base(id) != id || c == nil {
		return fmt.Errorf("unrecognized fixed synthetic victim")
	}
	private, err := readSynthetic(r.proc.path(c.Payload.PID, "root/work/custody-private"))
	if err != nil {
		if r.proc.gone(c.Payload.PID, c.Payload.StartTime, e.gate.Cgroup) {
			return nil
		}
		return fmt.Errorf("live victim private sentinel unavailable: %w", err)
	}
	if string(private) != privateSentinelValue(id) {
		return fmt.Errorf("victim private sentinel changed")
	}
	hostPath := filepath.Join("/state/cs5-sentinels/host", id)
	controlPath := filepath.Join("/state/cs5-sentinels/control", id)
	if err = ensureSentinel(hostPath, hostSentinelContent); err != nil {
		return err
	}
	if err = ensureSentinel(controlPath, controlSentinelContent); err != nil {
		return err
	}
	raw, err := readSynthetic(r.proc.path(c.Payload.PID, "root/work/custody-signals"))
	if err != nil {
		if r.proc.gone(c.Payload.PID, c.Payload.StartTime, e.gate.Cgroup) {
			return nil
		}
		return fmt.Errorf("live victim signal counter unavailable: %w", err)
	}
	if len(raw) == 0 {
		return nil
	} // transient truncate/write never yields positive proof
	count, err := strconv.Atoi(string(raw))
	if err != nil || count < 0 || count > 1 {
		return fmt.Errorf("victim signal counter invalid or changed")
	}
	if !e.signalSent {
		if count != 0 {
			return fmt.Errorf("positive victim signal already claimed before observer sent it")
		}
		if err = r.signalVictim(e); err != nil {
			return err
		}
		e.signalSent = true
		e.signalAt = time.Now().UnixNano()
	}
	e.sample.Sentinel = &SentinelObservation{HostPath: hostPath, ControlPath: controlPath, PrivateDigest: shaPayload(private), HostDigest: shaPayload([]byte(hostSentinelContent)), ControlDigest: shaPayload([]byte(controlSentinelContent)), SignalSentAt: e.signalAt, SignalCount: count}
	return nil
}
func (r *ResourceRecorder) signalVictim(e *resourceEntry) error {
	c := e.sample.Custody
	p := c.Payload
	if e.signalSent || p.PID <= 1 || p.PID == os.Getpid() || r.proc.root != "/proc" || time.Now().UnixNano()-c.ObservedAt > int64(250*time.Millisecond) {
		return fmt.Errorf("unsafe/stale positive signal target")
	}
	fd, err := unix.PidfdOpen(p.PID, 0)
	if err != nil {
		return fmt.Errorf("owned victim pidfd unavailable: %w", err)
	}
	defer unix.Close(fd)
	// The pidfd binds the process, and the complete post-open identity check binds
	// this one SIGUSR1 to the same owned lease, namespace, scope and start time.
	current, err := r.proc.identity(p.PID)
	if err != nil {
		return err
	}
	if current != p {
		return fmt.Errorf("victim identity changed before positive signal")
	}
	members, err := ids(filepath.Join(e.gate.Cgroup, "cgroup.threads"))
	if err != nil {
		return err
	}
	owned := false
	for _, id := range members {
		owned = owned || id == p.PID
	}
	if !owned || c.Mode != e.mode || c.Language != e.language || current.UIDs[0] != e.gate.HostUID || current.UserNS != e.gate.Namespaces["user"] {
		return fmt.Errorf("positive signal target outside fixed victim scope")
	}
	ownNS, err := os.Readlink("/proc/self/ns/user")
	if err != nil || ownNS == current.UserNS {
		return fmt.Errorf("positive signal target lacks private user namespace")
	}
	if time.Now().UnixNano()-c.ObservedAt > int64(250*time.Millisecond) {
		return fmt.Errorf("positive signal identity recheck deadline exceeded")
	}
	if err = unix.PidfdSendSignal(fd, unix.SIGUSR1, nil, 0); err != nil {
		return fmt.Errorf("owned victim signal control unavailable: %w", err)
	}
	return nil
}
func victimCode(language, id string) string {
	value := strconv.Quote(privateSentinelValue(id))
	if language == "python3" {
		return "import os,signal,time\nfrom jinja2 import Environment\nassert Environment().from_string('{{x|upper}}').render(x='custody')=='CUSTODY'\nvalue=" + value + "\nwith os.fdopen(os.open('/work/custody-private',os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o400),'w') as f: f.write(value)\nassert open('/work/custody-private').read()==value\ncount=0\nopen('/work/custody-signals','w').write('0')\ndef got_signal(*args):\n global count\n count+=1\n open('/work/custody-signals','w').write(str(count))\nsignal.signal(signal.SIGUSR1,got_signal)\nprint('CS1_READY',flush=True)\nprint('CS5_CUSTODY_VICTIM_READY',flush=True)\nwhile True:\n assert open('/work/custody-private').read()==value\n print('CS1_ALIVE',flush=True)\n time.sleep(.05)"
	}
	return "const fs=require('fs'),assert=require('assert');const value=" + value + ";fs.writeFileSync('/work/custody-private',value,{mode:0o400});assert.strictEqual(fs.readFileSync('/work/custody-private','utf8'),value);let count=0;fs.writeFileSync('/work/custody-signals','0');process.on('SIGUSR1',()=>{fs.writeFileSync('/work/custody-signals',String(++count));});console.log('CS1_READY');console.log('CS5_CUSTODY_VICTIM_READY');setInterval(()=>{assert.strictEqual(fs.readFileSync('/work/custody-private','utf8'),value);console.log('CS1_ALIVE')},50);"
}
func attackerCode(language string, paths []string, target ProcessIdentity) string {
	raw, _ := json.Marshal(paths)
	// Only the independently observed fixed victim leader is ever encoded. The
	// before/after proof binds PID reuse, namespace identity and lifetime.
	if target.PID <= 1 || target.TGID != target.PID || target.StartTime == "" || target.UserNS == "" || target.PIDNS == "" {
		return ""
	}
	binding, _ := json.Marshal(map[string]interface{}{"pid": target.PID, "tid": target.PID, "start": target.StartTime, "userns": target.UserNS, "pidns": target.PIDNS})
	if language == "python3" {
		return fmt.Sprintf(`import os,signal,json,time,ctypes
paths=%s
target=%s
libc=ctypes.CDLL('libc.so.6',use_errno=True)
libc.tgkill.argtypes=[ctypes.c_int,ctypes.c_int,ctypes.c_int]
libc.tgkill.restype=ctypes.c_int
libc.gettid.argtypes=[]
libc.gettid.restype=ctypes.c_int
signals=0
def got_signal(*args):
 global signals
 signals+=1
signal.signal(signal.SIGUSR1,got_signal)
open('/work/self-positive','w').write('CS5 self-positive')
self_read=open('/work/self-positive').read()
self_tid=libc.gettid()
assert self_tid==os.getpid()
assert libc.tgkill(os.getpid(),self_tid,int(signal.SIGUSR1))==0
time.sleep(.05)
reads=[]
for path in paths:
 try:
  with open(path,'rb') as f: f.read(256)
  error=0
 except OSError as e: error=e.errno
 reads.append(dict(path=path,errno=error))
ctypes.set_errno(0)
rc=libc.tgkill(target['pid'],target['tid'],int(signal.SIGUSR1))
signal_error=ctypes.get_errno() if rc==-1 else 0
assert rc in (-1,0)
print('CS1_READY',flush=True)
time.sleep(.1)
print(json.dumps(dict(reads=reads,signal_pid=target['pid'],signal_tid=target['tid'],signal_start=target['start'],signal_userns=target['userns'],signal_pidns=target['pidns'],signal_errno=signal_error,self_read=self_read,self_signals=signals)),flush=True)
`, raw, binding)
	}
	// Resolve the signal constant for this native Go target; importing Node's os
	// module after filtering performs unrelated operations excluded by the policy.
	return fmt.Sprintf(`const fs=require('fs'),koffi=require('koffi'),assert=require('assert');const paths=%s,target=%s;const libc=koffi.load('libc.so.6'),tgkill=libc.func('int tgkill(int,int,int)'),gettid=libc.func('int gettid()');const sig=%d;let signals=0;process.on('SIGUSR1',()=>signals++);fs.writeFileSync('/work/self-positive','CS5 self-positive');const self_read=fs.readFileSync('/work/self-positive','utf8');assert.strictEqual(gettid(),process.pid);assert.strictEqual(tgkill(process.pid,process.pid,sig),0);setTimeout(()=>{const codes={ENOENT:2,EACCES:13,EPERM:1,ESRCH:3};const reads=paths.map(path=>{try{fs.readFileSync(path);return{path,errno:0}}catch(e){return{path,errno:codes[e.code]||-1}}});koffi.errno(0);const rc=tgkill(target.pid,target.tid,sig),signal_errno=rc===-1?koffi.errno():0;assert(rc===-1||rc===0);console.log('CS1_READY');setTimeout(()=>console.log(JSON.stringify({reads,signal_pid:target.pid,signal_tid:target.tid,signal_start:target.start,signal_userns:target.userns,signal_pidns:target.pidns,signal_errno,self_read,self_signals:signals})),100)},50);`, raw, binding, int(unix.SIGUSR1))
}
func waitNative(ctx context.Context, directory, id, language, mode string, after int64, sentinel bool) (ResourceSample, error) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		raw, err := os.ReadFile(filepath.Join(directory, id+".resources.json"))
		var s ResourceSample
		if err == nil {
			if err = json.Unmarshal(raw, &s); err != nil {
				return s, err
			}
			if s.Error != "" {
				return s, fmt.Errorf("native observer: %s", s.Error)
			}
			if RequireNativeResourceSample(s) == nil && s.Custody.Language == language && s.Custody.Mode == mode && s.Custody.ObservedAt > after && (!sentinel || (s.Sentinel != nil && s.Sentinel.SignalCount == 1 && s.Sentinel.SignalSentAt < s.Custody.ObservedAt)) {
				return s, nil
			}
		}
		select {
		case <-ctx.Done():
			return s, fmt.Errorf("live native evidence unavailable: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// Behavioral controls are fixed synthetic workloads. No actual host secrets or
// control configuration are read, and no arbitrary signal target is accepted.
func IsolationControls(endpoint, profilePath, observations string) error {
	c, err := newEndpoint(endpoint, profilePath, observations)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(profilePath), "network.json"))
	if err != nil {
		return err
	}
	var fixture NetworkFixture
	if err = json.Unmarshal(raw, &fixture); err != nil {
		return err
	}
	if err = fixture.validate(); err != nil {
		return err
	}
	var target NetworkTarget
	for _, t := range fixture.Targets {
		if t.Protocol == "http" {
			target = t
			break
		}
	}
	for _, mode := range []string{"none", "scoped"} {
		for _, victimLanguage := range []string{"python3", "nodejs"} {
			if err = isolationControl(c, target, mode, victimLanguage); err != nil {
				return err
			}
		}
	}
	return nil
}
func isolationControl(c *endpointClient, target NetworkTarget, mode, victimLanguage string) (returnErr error) {
	attackerLanguage := "nodejs"
	if victimLanguage == "nodejs" {
		attackerLanguage = "python3"
	}
	scope := network.NetworkScope{Mode: mode}
	if mode == "scoped" {
		scope.Destinations = []network.Destination{{Protocol: target.Protocol, Host: target.Host, Port: target.Port, Addresses: []string{target.Address}}}
		scope.MaxEgressBytes = 65536
		scope.ExpiresAt = time.Now().Add(time.Duration(c.profile.Limits.WallMillis+5000) * time.Millisecond).Unix()
	}
	id := "custody-victim-" + mode + "-" + victimLanguage
	request := c.prepare(id, victimLanguage, victimCode(victimLanguage, "cs1-"+id), scope)
	type outcome struct {
		result isolation.ExecutionResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { result, err := c.run(request); done <- outcome{result, err} }()
	defer func() {
		cancelErr := c.cancel(request)
		select {
		case final := <-done:
			if returnErr == nil && (cancelErr != nil || final.err != nil || final.result.Status != "cancelled" || final.result.CleanupState != "clean") {
				returnErr = fmt.Errorf("synthetic victim did not cancel cleanly: %v %v %+v", cancelErr, final.err, final.result)
			}
		case <-time.After(time.Duration(c.profile.Limits.CleanupMillis+1000) * time.Millisecond):
			if returnErr == nil {
				returnErr = fmt.Errorf("synthetic victim cleanup deadline exceeded")
			}
		}
	}()
	ctx, stop := context.WithTimeout(context.Background(), time.Duration(c.profile.Limits.WallMillis)*time.Millisecond)
	defer stop()
	before, err := waitNative(ctx, c.observations, request.Options.ExecutionID, victimLanguage, mode, 0, true)
	if err != nil {
		return err
	}
	observation, err := readEvidence(c.observations, request.Options.ExecutionID)
	if err != nil || observation.ReadyNanos <= 0 {
		return fmt.Errorf("victim readiness absent: %v", err)
	}
	pid := before.Custody.Payload.PID
	paths := []string{fmt.Sprintf("/proc/%d/root/work/custody-private", pid), filepath.Join(observation.Root, "work/custody-private"), before.Sentinel.HostPath, before.Sentinel.ControlPath}
	began := time.Now().UnixNano()
	attack := c.prepare("custody-attacker-"+mode+"-"+victimLanguage, attackerLanguage, attackerCode(attackerLanguage, paths, before.Custody.Payload), scope)
	result, err := c.run(attack)
	if err != nil {
		return err
	}
	completed := time.Now().UnixNano()
	if result.Status != "succeeded" {
		return fmt.Errorf("native attacker/positive controls unavailable: %+v", result)
	}
	var attempt PeerAttack
	if err = json.Unmarshal([]byte(strings.TrimSpace(workloadOutput(result.Stdout))), &attempt); err != nil {
		return fmt.Errorf("actual attack report missing: %w", err)
	}
	attacker, err := readNativeResource(c.observations, attack.Options.ExecutionID, attackerLanguage, mode)
	if err != nil {
		return err
	}
	after, err := waitNative(ctx, c.observations, request.Options.ExecutionID, victimLanguage, mode, completed, true)
	if err != nil {
		return err
	}
	proof := PeerIsolationProof{Before: before, After: after, Attacker: attacker, Attempt: attempt, BeganAt: began, CompletedAt: completed, Paths: paths, PrivateDigest: shaPayload([]byte(privateSentinelValue(request.Options.ExecutionID))), HostDigest: shaPayload([]byte(hostSentinelContent)), ControlDigest: shaPayload([]byte(controlSentinelContent))}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	// Client-owned bounded artifacts; observer files remain read-only to clients.
	if directory := os.Getenv("CS5_ARTIFACTS"); directory != "" {
		if err = os.WriteFile(filepath.Join(directory, "isolation-"+mode+"-"+victimLanguage+".json"), raw, 0600); err != nil {
			return err
		}
	}
	return requirePeerIsolation(proof)
}
