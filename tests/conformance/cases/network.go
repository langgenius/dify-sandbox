package cases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"golang.org/x/net/dns/dnsmessage"
)

type NetworkTarget struct {
	Protocol, Address, Host string
	Port                    uint16
}
type NetworkFixture struct {
	CollectorURL              string
	Targets, AlternateTargets []NetworkTarget
}
type NetworkCounters struct {
	BootID        string            `json:"boot_id"`
	Counters      map[string]uint64 `json:"counters"`
	Active        map[string]uint64 `json:"active"`
	ReceivedBytes map[string]uint64 `json:"received_bytes"`
}

var trafficEvidence = struct {
	sync.Mutex
	Snapshots []struct {
		ObservedAt string          `json:"observed_at"`
		Evidence   NetworkCounters `json:"evidence"`
	}
}{}

func WriteNetworkEvidence(path string) error {
	trafficEvidence.Lock()
	defer trafficEvidence.Unlock()
	raw, err := json.MarshalIndent(trafficEvidence.Snapshots, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0600)
}

func (t NetworkTarget) key() string {
	return t.Protocol + "/" + t.Address + "/" + strconv.Itoa(int(t.Port))
}
func (t NetworkTarget) url(path string) string {
	return t.Protocol + "://" + net.JoinHostPort(t.Host, strconv.Itoa(int(t.Port))) + path
}
func (f NetworkFixture) validate() error {
	if f.CollectorURL == "" {
		return fmt.Errorf("independent collector required")
	}
	if len(f.Targets) != 10 || len(f.AlternateTargets) != 4 {
		return fmt.Errorf("exactly ten primary and four alternate targets required")
	}
	seen := map[string]bool{}
	for _, target := range f.Targets {
		ip := net.ParseIP(target.Address)
		if ip == nil || target.Port == 0 || target.Host == "" {
			return fmt.Errorf("invalid controlled target")
		}
		family := "v6"
		if ip.To4() != nil {
			family = "v4"
		}
		key := target.Protocol + "/" + family
		if seen[key] {
			return fmt.Errorf("duplicate protocol/family control")
		}
		seen[key] = true
	}
	for _, protocol := range []string{"tcp", "udp", "dns", "http", "https"} {
		for _, family := range []string{"v4", "v6"} {
			if !seen[protocol+"/"+family] {
				return fmt.Errorf("missing permissive %s/%s control", protocol, family)
			}
		}
	}
	alternates := map[string]bool{}
	for _, target := range f.AlternateTargets {
		ip := net.ParseIP(target.Address)
		if ip == nil || target.Port == 0 || target.Host == "" || (target.Protocol != "http" && target.Protocol != "https") {
			return fmt.Errorf("invalid alternate target")
		}
		family := "v6"
		if ip.To4() != nil {
			family = "v4"
		}
		alternates[target.Protocol+"/"+family] = true
		for _, approved := range f.Targets {
			if target.key() == approved.key() {
				return fmt.Errorf("alternate target is approved")
			}
		}
	}
	for _, protocol := range []string{"http", "https"} {
		for _, family := range []string{"v4", "v6"} {
			if !alternates[protocol+"/"+family] {
				return fmt.Errorf("missing alternate %s/%s", protocol, family)
			}
		}
	}
	return nil
}
func collectNetwork(client *http.Client, fixture NetworkFixture) (NetworkCounters, error) {
	var result NetworkCounters
	response, err := client.Get(fixture.CollectorURL)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return result, fmt.Errorf("collector HTTP%d", response.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return result, err
	}
	if result.BootID == "" || result.Counters == nil || result.Active == nil || result.ReceivedBytes == nil {
		return result, fmt.Errorf("missing independent traffic evidence")
	}
	for _, target := range append(append([]NetworkTarget{}, fixture.Targets...), fixture.AlternateTargets...) {
		if _, ok := result.Counters[target.key()]; !ok {
			return result, fmt.Errorf("missing counter %s", target.key())
		}
		if _, ok := result.Active[target.key()]; !ok {
			return result, fmt.Errorf("missing active count %s", target.key())
		}
		if _, ok := result.ReceivedBytes[target.key()]; !ok {
			return result, fmt.Errorf("missing received bytes %s", target.key())
		}
	}
	trafficEvidence.Lock()
	defer trafficEvidence.Unlock()
	if len(trafficEvidence.Snapshots) >= 4096 {
		return result, fmt.Errorf("bounded network evidence capacity exhausted")
	}
	trafficEvidence.Snapshots = append(trafficEvidence.Snapshots, struct {
		ObservedAt string          `json:"observed_at"`
		Evidence   NetworkCounters `json:"evidence"`
	}{time.Now().UTC().Format(time.RFC3339Nano), result})
	return result, nil
}
func compareNetwork(before, after NetworkCounters, targets []NetworkTarget, positive bool) error {
	if before.BootID == "" || before.BootID != after.BootID {
		return fmt.Errorf("collector restarted or missing boot identity")
	}
	for _, target := range targets {
		a, okA := before.Counters[target.key()]
		b, okB := after.Counters[target.key()]
		if !okA || !okB || b < a {
			return fmt.Errorf("missing/regressed counter %s", target.key())
		}
		if positive && b == a {
			return fmt.Errorf("permissive control did not reach %s", target.key())
		}
		if !positive && b != a {
			return fmt.Errorf("denied traffic reached %s", target.key())
		}
	}
	return nil
}
func settledNetwork(client *http.Client, fixture NetworkFixture, baseline NetworkCounters) (NetworkCounters, error) {
	deadline := time.Now().Add(3 * time.Second)
	for {
		now, err := collectNetwork(client, fixture)
		if err != nil {
			return now, err
		}
		if now.BootID != baseline.BootID {
			return now, fmt.Errorf("collector restarted")
		}
		idle := true
		for key, n := range baseline.Active {
			idle = idle && now.Active[key] == n
		}
		if idle {
			return now, nil
		}
		if time.Now().After(deadline) {
			return now, fmt.Errorf("live connections survived cleanup")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// permissiveProbe is deliberately outside the candidate payload namespace. Its
// successful independent counters are mandatory before any denial is accepted.
func permissiveProbe(target NetworkTarget) error {
	address := net.JoinHostPort(target.Address, strconv.Itoa(int(target.Port)))
	if target.Protocol == "http" || target.Protocol == "https" {
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", address)
		}} // synthetic fixture certificates only
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Get(target.url("/ok"))
		if err != nil {
			return err
		}
		defer response.Body.Close()
		io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		if response.StatusCode != 200 {
			return fmt.Errorf("control HTTP%d", response.StatusCode)
		}
		return nil
	}
	protocol := "tcp"
	if target.Protocol == "udp" || target.Protocol == "dns" {
		protocol = "udp"
	}
	connection, err := net.DialTimeout(protocol, address, time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(time.Second))
	payload := []byte("CS4_SYNTHETIC_CONTROL")
	if target.Protocol == "dns" {
		payload = []byte{0x43, 0x34, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0}
		for _, label := range strings.Split("cs4-control.fixture.test", ".") {
			payload = append(payload, byte(len(label)))
			payload = append(payload, []byte(label)...)
		}
		payload = append(payload, 0, 0, 1, 0, 1)
	}
	_, err = connection.Write(payload)
	return err
}

// Network fails on missing kernel fixture, positive controls, real observations
// or live-connection settlement. Unit/component tests cannot satisfy this case.
func Network(endpoint, profilePath, observations string) error {
	raw, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("qualified network profile required: %w", err)
	}
	var profile isolation.Profile
	if err = json.Unmarshal(raw, &profile); err != nil {
		return err
	}
	if err = profile.Validate(); err != nil {
		return err
	}
	if !profile.HTTPProxyQualified || profile.MaxNetworkEgressBytes < 65536 || profile.MaxConcurrent < 2 || profile.Limits.WallMillis <= 3250+2*profile.Limits.CleanupMillis {
		return fmt.Errorf("qualified proxy, 64KiB egress, two slots and wall limit later than expiry plus cleanup required")
	}
	raw, err = os.ReadFile(filepath.Join(filepath.Dir(profilePath), "network.json"))
	if err != nil {
		return fmt.Errorf("controlled network fixture required: %w", err)
	}
	var fixture NetworkFixture
	if err = json.Unmarshal(raw, &fixture); err != nil {
		return err
	}
	if err = fixture.validate(); err != nil {
		return err
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Duration(profile.Limits.WallMillis+2*profile.Limits.CleanupMillis+5000) * time.Millisecond}
	baseline, err := collectNetwork(client, fixture)
	if err != nil {
		return err
	}
	for _, target := range fixture.Targets {
		if err = permissiveProbe(target); err != nil {
			return fmt.Errorf("permissive %s: %w", target.key(), err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	var controlled NetworkCounters
	for {
		controlled, err = settledNetwork(client, fixture, baseline)
		if err != nil {
			return err
		}
		if err = compareNetwork(baseline, controlled, fixture.Targets, true); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
	type execution struct {
		request isolation.InvocationRequest
		result  isolation.ExecutionResult
		err     error
	}
	prepare := func(id, code string, scope network.NetworkScope) isolation.InvocationRequest {
		code += "\nimport time\nprint('CS1_READY',flush=True)\ntime.sleep(.1)\n"
		payload, _ := json.Marshal(isolation.Payload{Code: code, Preload: ""})
		if scope.Mode == "scoped" {
			digest := sha256.Sum256(payload)
			scope.InputManifestDigest = hex.EncodeToString(digest[:])
			scope.ScopeDigest = network.ScopeDigest(scope)
		}
		spec := isolation.InvocationSpec{Version: 1, ExecutionID: "cs1-network-" + id, Language: "python3", EnvironmentDigest: profile.EnvironmentDigest, ProfileDigest: profile.Digest, BudgetReservationRef: "cs1-synthetic", Budget: profile.Limits, Network: scope}
		spec.InvocationDigest = isolation.InvocationDigest(spec, payload)
		return isolation.InvocationRequest{Options: spec, Code: code, Language: "python3"}
	}
	run := func(request isolation.InvocationRequest) (isolation.ExecutionResult, error) {
		body, _ := json.Marshal(request)
		req, _ := http.NewRequest("POST", endpoint+"/v1/sandbox/run", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
		response, err := client.Do(req)
		if err != nil {
			return isolation.ExecutionResult{}, err
		}
		defer response.Body.Close()
		var result isolation.ExecutionResult
		if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
			return result, err
		}
		if response.StatusCode != 200 {
			return result, fmt.Errorf("candidate HTTP%d: %s", response.StatusCode, result.FailureReason)
		}
		evidence, err := readEvidence(observations, request.Options.ExecutionID)
		if err != nil {
			return result, err
		}
		if evidence.Phase != "completed" || !evidence.RootRemoved || !evidence.CgroupRemoved || result.CleanupState != "clean" || evidence.Namespaces["net"] == "" {
			return result, fmt.Errorf("network execution lacks real namespace/cleanup evidence")
		}
		if _, err := readNativeResource(observations, request.Options.ExecutionID, "python3", request.Options.Network.Mode); err != nil {
			return result, err
		}
		if request.Options.Network.Mode == "scoped" {
			if evidence.Namespaces["user"] == "" || evidence.HostUID < profile.HostUIDMin || evidence.HostUID >= profile.HostUIDMax {
				return result, fmt.Errorf("missing scoped user namespace/UID lease evidence")
			}
			mapped := false
			for _, line := range strings.Split(evidence.Controls["uid_map"], "\n") {
				fields := strings.Fields(line)
				if len(fields) == 3 && fields[0] == "65537" && fields[1] == strconv.Itoa(evidence.HostUID) && fields[2] == "1" {
					mapped = true
				}
			}
			if !mapped {
				return result, fmt.Errorf("namespace payload UID does not map to owned lease")
			}
		}
		return result, nil
	}
	targets, _ := json.Marshal(fixture.Targets)
	noneCode := fmt.Sprintf(`import json,socket,ssl
for t in json.loads(%q):
 try:
  family=socket.AF_INET6 if ':' in t['Address'] else socket.AF_INET
  kind=socket.SOCK_DGRAM if t['Protocol'] in ('udp','dns') else socket.SOCK_STREAM
  s=socket.socket(family,kind);s.settimeout(0.3)
  if kind==socket.SOCK_DGRAM:s.sendto(b'CS4_SYNTHETIC_NONE',(t['Address'],t['Port']))
  else:
   s.connect((t['Address'],t['Port']))
   if t['Protocol']=='https':s=ssl._create_unverified_context().wrap_socket(s,server_hostname=t['Host'])
   s.sendall(b'GET /ok HTTP/1.0\r\n\r\n' if t['Protocol'] in ('http','https') else b'CS4_SYNTHETIC_NONE')
  s.close()
 except OSError:pass
print('NONE_ATTEMPTED')`, string(targets))
	result, err := run(prepare("none", noneCode, network.NetworkScope{Mode: "none"}))
	if err != nil {
		return err
	}
	if result.Status != "succeeded" || !strings.Contains(result.Stdout, "NONE_ATTEMPTED") {
		return fmt.Errorf("none workload did not execute every probe: %+v", result)
	}
	afterNone, err := settledNetwork(client, fixture, baseline)
	if err != nil {
		return err
	}
	if err = compareNetwork(controlled, afterNone, fixture.Targets, false); err != nil {
		return err
	}
	scopeFor := func(target NetworkTarget) network.NetworkScope {
		return network.NetworkScope{Mode: "scoped", Destinations: []network.Destination{{Protocol: target.Protocol, Host: target.Host, Port: target.Port, Addresses: []string{target.Address}}}, MaxEgressBytes: 65536, ExpiresAt: time.Now().Add(time.Duration(profile.Limits.WallMillis+5000) * time.Millisecond).Unix()}
	}
	get := func(target NetworkTarget, path string) string {
		return fmt.Sprintf("requests.get(%q,verify=False,timeout=3)", target.url(path))
	}
	for i, target := range fixture.Targets {
		if target.Protocol != "http" && target.Protocol != "https" {
			continue
		}
		code := "import requests\nassert " + get(target, "/ok") + ".status_code==200\nprint('SCOPED_OK')"
		result, err = run(prepare("allowed-"+strconv.Itoa(i), code, scopeFor(target)))
		if err != nil {
			return err
		}
		if result.Status != "succeeded" || !strings.Contains(result.Stdout, "SCOPED_OK") || result.NetworkReceipt == nil || result.NetworkReceipt.EgressBytes <= 0 || result.NetworkReceipt.EgressBytes > 65536 {
			return fmt.Errorf("approved scoped traffic failed: %+v", result)
		}
	}
	negativeBaseline, err := settledNetwork(client, fixture, baseline)
	if err != nil {
		return err
	}
	primary := NetworkTarget{}
	for _, target := range fixture.Targets {
		if target.Protocol == "http" && net.ParseIP(target.Address).To4() != nil {
			primary = target
			break
		}
	}
	for i, alternate := range fixture.AlternateTargets {
		code := "import requests\nassert " + get(primary, "/ok") + ".status_code==200\ntry:\n r=" + get(alternate, "/ok") + "\n assert r.status_code==403\nexcept requests.exceptions.ProxyError:pass\nprint('TARGET_DENIED')"
		result, err = run(prepare("alternate-"+strconv.Itoa(i), code, scopeFor(primary)))
		if err != nil {
			return err
		}
		if result.Status != "succeeded" || !strings.Contains(result.Stdout, "TARGET_DENIED") {
			return fmt.Errorf("alternate target denial not exercised: %+v", result)
		}
	}
	code := "import requests\nassert " + get(primary, "/ok") + ".status_code==200\ntry:\n r=" + get(primary, "/redirect-unapproved") + "\n assert r.status_code==403\nexcept requests.exceptions.ProxyError:pass\nprint('REDIRECT_DENIED')"
	result, err = run(prepare("redirect", code, scopeFor(primary)))
	if err != nil {
		return err
	}
	if result.Status != "succeeded" || !strings.Contains(result.Stdout, "REDIRECT_DENIED") {
		return fmt.Errorf("redirect not rechecked: %+v", result)
	}
	afterDenied, err := settledNetwork(client, fixture, baseline)
	if err != nil {
		return err
	}
	if err = compareNetwork(negativeBaseline, afterDenied, fixture.AlternateTargets, false); err != nil {
		return err
	}
	// Different concurrent scopes must never share outbound authority.
	alternate := NetworkTarget{}
	for _, target := range fixture.AlternateTargets {
		if target.Protocol == "http" && net.ParseIP(target.Address).To4() != nil {
			alternate = target
			break
		}
	}
	concurrent := make(chan execution, 2)
	for i, pair := range [][2]NetworkTarget{{primary, alternate}, {alternate, primary}} {
		code := "import requests,time\nassert " + get(pair[0], "/ok") + ".status_code==200\nprint('CS1_READY',flush=True)\ntime.sleep(1)\nassert " + get(pair[1], "/ok") + ".status_code==403\nprint('ISOLATED_SCOPE')"
		request := prepare("concurrent-"+strconv.Itoa(i), code, scopeFor(pair[0]))
		go func(request isolation.InvocationRequest) {
			result, err := run(request)
			concurrent <- execution{request: request, result: result, err: err}
		}(request)
	}
	peers := []Evidence{}
	for i := 0; i < 2; i++ {
		done := <-concurrent
		if done.err != nil {
			return done.err
		}
		if done.result.Status != "succeeded" || !strings.Contains(done.result.Stdout, "ISOLATED_SCOPE") {
			return fmt.Errorf("concurrent scope isolation failed: %+v", done.result)
		}
		evidence, err := readEvidence(observations, done.request.Options.ExecutionID)
		if err != nil {
			return err
		}
		peers = append(peers, evidence)
	}
	if err = requireOverlap(peers[0], peers[1]); err != nil {
		return err
	}
	if peers[0].HostUID == peers[1].HostUID || peers[0].Namespaces["user"] == peers[1].Namespaces["user"] || peers[0].Namespaces["net"] == peers[1].Namespaces["net"] {
		return fmt.Errorf("concurrent scopes shared UID or namespace")
	}
	if _, err = settledNetwork(client, fixture, baseline); err != nil {
		return err
	}
	// The protected metadata owner captures the first DNS answer. A subsequent
	// changed answer must not influence the gateway's authenticated literal pin.
	dnsTarget := NetworkTarget{}
	for _, target := range fixture.Targets {
		if target.Protocol == "dns" && net.ParseIP(target.Address).To4() != nil {
			dnsTarget = target
			break
		}
	}
	first, err := rebindControl(dnsTarget)
	if err != nil {
		return err
	}
	second, err := rebindControl(dnsTarget)
	if err != nil {
		return err
	}
	if first != primary.Address || second == first {
		return fmt.Errorf("DNS rebinding positive control did not change the approved address")
	}
	rebound := primary
	rebound.Host = "rebind.fixture.test"
	beforeRebind, err := collectNetwork(client, fixture)
	if err != nil {
		return err
	}
	result, err = run(prepare("rebind", "import requests\nassert "+get(rebound, "/ok")+".status_code==200\nprint('PIN_RETAINED')", scopeFor(rebound)))
	if err != nil {
		return err
	}
	if result.Status != "succeeded" || !strings.Contains(result.Stdout, "PIN_RETAINED") {
		return fmt.Errorf("immutable DNS pin was not retained: %+v", result)
	}
	afterRebind, err := settledNetwork(client, fixture, baseline)
	if err != nil {
		return err
	}
	if err = compareNetwork(beforeRebind, afterRebind, fixture.AlternateTargets, false); err != nil {
		return err
	}
	if err = compareNetwork(beforeRebind, afterRebind, []NetworkTarget{dnsTarget}, false); err != nil {
		return err
	}
	// Egress quota counts outbound request headers/body (not response bytes).
	beforeQuota, err := collectNetwork(client, fixture)
	if err != nil {
		return err
	}
	quota := scopeFor(primary)
	quota.MaxEgressBytes = 128
	result, err = run(prepare("quota", "import requests\nprint('QUOTA_ATTEMPT_4096',flush=True)\ntry:\n requests.post("+strconv.Quote(primary.url("/ok"))+",data=b'x'*4096,timeout=3)\nexcept requests.exceptions.RequestException:pass\nprint('QUOTA_EXERCISED')", quota))
	if err != nil {
		return err
	}
	afterQuota, err := settledNetwork(client, fixture, baseline)
	if err != nil {
		return err
	}
	if err = quotaEvidence(result, beforeQuota, afterQuota, primary, 128); err != nil {
		return err
	}
	hold := "import requests\nr=requests.get(" + strconv.Quote(primary.url("/hold")) + ",stream=True,timeout=10)\nassert r.status_code==200\nprint('CS1_READY',flush=True)\nfor chunk in r.iter_content(1):pass"
	expire := scopeFor(primary)
	expire.ExpiresAt = time.Now().Add(3 * time.Second).Unix()
	expiryRequest := prepare("expiry", hold, expire)
	// Convert UTC scope expiry once; all subsequent elapsed bounds use the
	// monotonic clock retained by started and time.Since(started).
	started := time.Now()
	expiresAfter := time.Unix(expire.ExpiresAt, 0).Sub(started)
	allowance := 2*time.Duration(profile.Limits.CleanupMillis)*time.Millisecond + 250*time.Millisecond
	wall := time.Duration(profile.Limits.WallMillis) * time.Millisecond
	if expiresAfter <= 0 || wall <= expiresAfter+allowance {
		return fmt.Errorf("ordinary wall deadline does not distinguish scope expiry")
	}
	expiryDone := make(chan execution, 1)
	go func() { r, e := run(expiryRequest); expiryDone <- execution{request: expiryRequest, result: r, err: e} }()
	liveContext, stopLive := context.WithDeadline(context.Background(), started.Add(expiresAfter))
	readyErr := waitReady(liveContext, observations, expiryRequest.Options.ExecutionID)
	live, liveAt := false, time.Duration(0)
	if readyErr == nil {
		for liveContext.Err() == nil {
			snapshot, e := collectNetwork(client, fixture)
			if e != nil {
				readyErr = e
				break
			}
			if snapshot.BootID != baseline.BootID {
				readyErr = fmt.Errorf("collector restarted before live expiry proof")
				break
			}
			if snapshot.Active[primary.key()] > baseline.Active[primary.key()] {
				live = true
				liveAt = time.Since(started)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	stopLive()
	expired := <-expiryDone
	if expired.err != nil {
		return expired.err
	}
	if _, err = settledNetwork(client, fixture, baseline); err != nil {
		return err
	}
	if err = expiryEvidence(expired.result, readyErr == nil, live, liveAt, time.Since(started), expiresAfter, allowance, wall); err != nil {
		return err
	}
	request := prepare("revoke", hold, scopeFor(primary))
	done := make(chan execution, 1)
	go func() { r, e := run(request); done <- execution{request: request, result: r, err: e} }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(profile.Limits.WallMillis)*time.Millisecond)
	defer cancel()
	if err = waitReady(ctx, observations, request.Options.ExecutionID); err != nil {
		<-done
		return err
	}
	req, _ := http.NewRequest("POST", endpoint+"/v1/sandbox/executions/"+request.Options.ExecutionID+"/cancel", nil)
	req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
	req.Header.Set("X-Invocation-Digest", request.Options.InvocationDigest)
	response, err := client.Do(req)
	if err != nil {
		<-done
		return err
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		<-done
		return fmt.Errorf("revoke refused")
	}
	stopped := <-done
	if stopped.err != nil {
		return stopped.err
	}
	if stopped.result.Status != "cancelled" {
		return fmt.Errorf("revocation did not cancel execution")
	}
	_, err = settledNetwork(client, fixture, baseline)
	return err
}

func rebindControl(target NetworkTarget) (string, error) {
	name, err := dnsmessage.NewName("rebind.fixture.test.")
	if err != nil {
		return "", err
	}
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: 0x4334, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
	raw, err := query.Pack()
	if err != nil {
		return "", err
	}
	conn, err := net.DialTimeout("udp", net.JoinHostPort(target.Address, strconv.Itoa(int(target.Port))), time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Second))
	if _, err = conn.Write(raw); err != nil {
		return "", err
	}
	response := make([]byte, 4096)
	n, err := conn.Read(response)
	if err != nil {
		return "", err
	}
	var answer dnsmessage.Message
	if err = answer.Unpack(response[:n]); err != nil {
		return "", err
	}
	if answer.ID != query.ID || !answer.Response || answer.RCode != dnsmessage.RCodeSuccess {
		return "", fmt.Errorf("invalid DNS control response")
	}
	for _, resource := range answer.Answers {
		if a, ok := resource.Body.(*dnsmessage.AResource); ok {
			return net.IP(a.A[:]).String(), nil
		}
	}
	return "", fmt.Errorf("missing control DNS address")
}

// These assertions are shared with adversarial evidence regressions.
func quotaEvidence(result isolation.ExecutionResult, before, after NetworkCounters, target NetworkTarget, limit int64) error {
	receipt := result.NetworkReceipt
	if limit <= 0 || result.Status != "succeeded" || !strings.Contains(result.Stdout, "QUOTA_ATTEMPT_4096") || !strings.Contains(result.Stdout, "QUOTA_EXERCISED") || receipt == nil || !receipt.Revoked || receipt.RevocationReason != "egress_quota" || receipt.EgressBytes < 0 || receipt.EgressBytes > limit || receipt.DeniedEgressBytes <= limit-receipt.EgressBytes {
		return fmt.Errorf("quota workload or nonroutine rejection evidence missing")
	}
	if err := compareNetwork(before, after, []NetworkTarget{target}, true); err != nil {
		return err
	}
	a, okA := before.ReceivedBytes[target.key()]
	b, okB := after.ReceivedBytes[target.key()]
	if !okA || !okB || b < a || b-a > uint64(limit) || b-a > uint64(receipt.EgressBytes) {
		return fmt.Errorf("independent quota byte ceiling missing or exceeded")
	}
	return nil
}
func expiryEvidence(result isolation.ExecutionResult, ready, live bool, liveAt, finished, expires, allowance, wall time.Duration) error {
	if result.Status != "failed" || result.FailureReason != "deadline" || !ready || !live || liveAt <= 0 || liveAt >= expires || expires <= 0 || allowance < 0 || finished < expires || finished > expires+allowance || wall <= expires+allowance {
		return fmt.Errorf("live scope expiry or bounded teardown not proved")
	}
	return nil
}
