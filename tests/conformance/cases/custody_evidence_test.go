package cases

import (
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"testing"
)

func completedCustody() Evidence {
	return Evidence{Observation: isolation.Observation{ExecutionID: "cs1-real", Phase: "completed", HostUID: 20000, RootRemoved: true, CgroupRemoved: true,
		Namespaces: map[string]string{"mnt": "mnt:[1]", "pid": "pid:[2]", "ipc": "ipc:[3]", "uts": "uts:[4]", "net": "net:[5]", "user": "user:[6]"},
		Controls:   map[string]string{"uid_map": "0 999 1\n65537 20000 1", "gid_map": "0 999 1"}, Result: isolation.ExecutionResult{CleanupState: "clean"}}}
}
func TestLifecycleAcceptsSixNamedNamespacesAndRejectsMissingCustody(t *testing.T) {
	if err := requireLifecycleCompletion(completedCustody()); err != nil {
		t.Fatal("valid six-namespace completion rejected:", err)
	}
	for _, bad := range []string{"user", "mnt", "pid", "ipc", "uts", "net", "uid"} {
		t.Run(bad, func(t *testing.T) {
			e := completedCustody()
			if bad == "uid" {
				e.HostUID = 65537
			} else {
				e.Namespaces[bad] = ""
			}
			if requireLifecycleCompletion(e) == nil {
				t.Fatal("missing live custody prerequisite accepted")
			}
		})
	}
}

func nativeSample() ResourceSample {
	p := ProcessIdentity{PID: 100, TGID: 100, NamespacePID: 1, StartTime: "1234", UserNS: "user:[6]", PIDNS: "pid:[2]", Cgroup: "0::/owned/request-a", Executable: "/usr/bin/python3.12", UIDs: [4]int{20000, 20000, 20000, 20000}, GIDs: [4]int{999, 999, 999, 999}}
	return ResourceSample{Samples: 2, NativeSamples: 2, PIDs: 2, FDs: 8, DiskBytes: 4096, IntervalMicros: 2000, Custody: &NativeCustody{Language: "python3", Mode: "none", HostUID: 20000, HostGID: 999, UserNS: "user:[6]", PIDNS: "pid:[2]", Cgroup: "0::/owned/request-a", GatedPayload: p, Payload: p, RequiredThreads: []int{100}, Threads: []ProcessIdentity{p}, ObservedAt: 12345}}
}
func TestNativeEvidenceBindsActualPayloadAndAllThreads(t *testing.T) {
	for _, mode := range []string{"none", "scoped"} {
		for _, lang := range []string{"python3", "nodejs"} {
			t.Run(mode+"/"+lang, func(t *testing.T) {
				base := nativeSample()
				base.Custody.Mode = mode
				base.Custody.Language = lang
				if lang == "nodejs" {
					base.Custody.Payload.Executable = "/usr/bin/node"
					base.Custody.Threads[0].Executable = "/usr/bin/node"
				}
				if err := RequireNativeResourceSample(base); err != nil {
					t.Fatal(err)
				}
				for _, bad := range []string{"namespace-root-zero-caps", "wrong-live-uid", "helper-substitution", "payload-caps", "thread-caps", "unreadable-live-thread", "reused-pid", "wrong-userns", "wrong-gid"} {
					t.Run(bad, func(t *testing.T) {
						s := nativeSample()
						s.Custody.Mode = mode
						s.Custody.Language = lang
						if lang == "nodejs" {
							s.Custody.Payload.Executable = "/usr/bin/node"
							s.Custody.Threads[0].Executable = "/usr/bin/node"
						}
						switch bad {
						case "namespace-root-zero-caps":
							s.Custody.Payload.UIDs = [4]int{999, 999, 999, 999}
						case "wrong-live-uid":
							s.Custody.Payload.UIDs[0] = 20001
						case "helper-substitution":
							s.Custody.Payload.PID = 101
							s.Custody.Payload.TGID = 101
							s.Custody.Payload.NamespacePID = 2
							s.Custody.Payload.Executable = "/usr/bin/network-gateway"
						case "payload-caps":
							s.Custody.Payload.CapPrm = 1
						case "thread-caps":
							thread := s.Custody.Payload
							thread.PID = 102
							thread.CapAmb = 1
							s.Custody.RequiredThreads = append(s.Custody.RequiredThreads, 102)
							s.Custody.Threads = append(s.Custody.Threads, thread)
						case "unreadable-live-thread":
							s.Custody.RequiredThreads = append(s.Custody.RequiredThreads, 102)
						case "reused-pid":
							s.Custody.Payload.StartTime = "5678"
						case "wrong-userns":
							s.Custody.Payload.UserNS = "user:[0]"
						case "wrong-gid":
							s.Custody.Payload.GIDs[1] = 0
						}
						if RequireNativeResourceSample(s) == nil {
							t.Fatal("unsafe/incomplete live payload accepted")
						}
					})
				}
			})
		}
	}
}

func peerProof() PeerIsolationProof {
	before := nativeSample()
	before.Custody.ObservedAt = 100
	before.Sentinel = &SentinelObservation{HostPath: "/state/cs5-sentinels/host/victim", ControlPath: "/state/cs5-sentinels/control/victim", PrivateDigest: "private", HostDigest: "host", ControlDigest: "control", SignalSentAt: 90, SignalCount: 1}
	after := nativeSample()
	after.Custody.ObservedAt = 300
	copySentinel := *before.Sentinel
	after.Sentinel = &copySentinel
	attacker := nativeSample()
	attacker.Custody.ObservedAt = 200
	attacker.Custody.HostUID = 20001
	attacker.Custody.UserNS = "user:[7]"
	attacker.Custody.PIDNS = "pid:[7]"
	attacker.Custody.GatedPayload.PID = 101
	attacker.Custody.Payload.PID = 101
	attacker.Custody.Payload.TGID = 101
	attacker.Custody.Payload.UserNS = "user:[7]"
	attacker.Custody.Payload.PIDNS = "pid:[7]"
	attacker.Custody.Payload.UIDs = [4]int{20001, 20001, 20001, 20001}
	attacker.Custody.Threads = []ProcessIdentity{attacker.Custody.Payload}
	attacker.Custody.RequiredThreads = []int{101}
	paths := []string{"/proc/100/root/work/custody-private", "/state/work/request-victim/work/custody-private", "/state/cs5-sentinels/host/victim", "/state/cs5-sentinels/control/victim"}
	return PeerIsolationProof{Before: before, After: after, Attacker: attacker, BeganAt: 150, CompletedAt: 250, Paths: paths, PrivateDigest: "private", HostDigest: "host", ControlDigest: "control", Attempt: PeerAttack{Reads: []ReadAttempt{{paths[0], 2}, {paths[1], 2}, {paths[2], 2}, {paths[3], 2}}, SignalPID: 100, SignalTID: 100, SignalStart: "1234", SignalUserNS: "user:[6]", SignalPIDNS: "pid:[2]", SignalErrno: 3, SelfRead: "CS5 self-positive", SelfSignals: 1}}
}
func TestPeerIsolationRejectsMissingOrUnavailableBehavioralEvidence(t *testing.T) {
	if err := requirePeerIsolation(peerProof()); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"not-ready", "missing-live-victim", "missing-positive-signal", "signal-changed", "sentinel-changed", "no-positive-read", "no-positive-self-signal", "probe-missing", "probe-unavailable", "read-succeeded", "signal-succeeded", "wrong-target", "wrong-thread", "wrong-start", "wrong-target-userns", "wrong-target-pidns", "victim-ended-before-attack", "same-uid"} {
		t.Run(bad, func(t *testing.T) {
			p := peerProof()
			switch bad {
			case "not-ready":
				p.Before.NativeSamples = 0
			case "missing-live-victim":
				p.After.Custody = nil
			case "missing-positive-signal":
				p.Before.Sentinel.SignalSentAt = 0
			case "signal-changed":
				p.After.Sentinel.SignalCount = 2
			case "sentinel-changed":
				p.After.Sentinel.PrivateDigest = "changed"
			case "no-positive-read":
				p.Attempt.SelfRead = ""
			case "no-positive-self-signal":
				p.Attempt.SelfSignals = 0
			case "probe-missing":
				p.Attempt.Reads = p.Attempt.Reads[:3]
			case "probe-unavailable":
				p.Attempt.Reads[0].Errno = 38
			case "read-succeeded":
				p.Attempt.Reads[0].Errno = 0
			case "signal-succeeded":
				p.Attempt.SignalErrno = 0
			case "wrong-target":
				p.Attempt.SignalPID = 1
			case "wrong-thread":
				p.Attempt.SignalTID = 101
			case "wrong-start":
				p.Attempt.SignalStart = "5678"
			case "wrong-target-userns":
				p.Attempt.SignalUserNS = "user:[0]"
			case "wrong-target-pidns":
				p.Attempt.SignalPIDNS = "pid:[0]"
			case "victim-ended-before-attack":
				p.After.Custody.ObservedAt = 200
			case "same-uid":
				p.Attacker.Custody.HostUID = p.Before.Custody.HostUID
			}
			if requirePeerIsolation(p) == nil {
				t.Fatal("missing or unsafe actual isolation control accepted")
			}
		})
	}
}
