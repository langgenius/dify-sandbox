package cases

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
)

// This fixed fixture client explicitly uses HTTP_PROXY/CONNECT. Node's builtin
// http client does not automatically honor proxy variables; support is not
// claimed for arbitrary proxy-unaware clients.
const nodeProxyClient = `const http=require('http'),https=require('https'),tls=require('tls'),assert=require('assert');
const proxy=new URL(process.env.HTTP_PROXY);
function request(url,body='',hold=false){return new Promise((resolve,reject)=>{
 const target=new URL(url);
 const finish=r=>{if(hold){assert.strictEqual(r.statusCode,200);console.log('CS1_READY');}
  r.on('data',()=>{});r.on('error',reject);r.on('end',()=>resolve(r.statusCode));};
 const options={method:body?'POST':'GET',headers:{Host:target.host,Connection:'close','Content-Length':Buffer.byteLength(body)},timeout:12000};
 const send=q=>{q.on('timeout',()=>q.destroy(Error('timeout')));q.on('error',reject);q.end(body);};
 if(target.protocol==='http:'){send(http.request({...options,hostname:proxy.hostname,port:proxy.port,path:target.href},finish));return;}
 const connect=http.request({hostname:proxy.hostname,port:proxy.port,method:'CONNECT',path:target.host,headers:{Host:target.host},timeout:12000});
 connect.on('connect',(response,socket,head)=>{if(response.statusCode!==200){socket.destroy();resolve(response.statusCode);return;}
  if(head.length)socket.unshift(head);
  const stream=tls.connect({socket,servername:target.hostname.replace(/^\[|\]$/g,''),rejectUnauthorized:false});
  stream.on('error',reject);
  stream.on('secureConnect',()=>{const agent=new https.Agent({keepAlive:false});agent.createConnection=()=>stream;
   const q=https.request({...options,hostname:target.hostname.replace(/^\[|\]$/g,''),port:target.port,path:target.pathname+target.search,agent},finish);
   q.on('close',()=>agent.destroy());send(q);});});
 connect.on('timeout',()=>connect.destroy(Error('timeout')));connect.on('error',reject);connect.end();
});}
`

func NetworkNode(endpoint, profilePath, observations string) error {
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
	baseline, err := collectNetwork(c.http, fixture)
	if err != nil {
		return err
	}
	for _, t := range fixture.Targets {
		if err = permissiveProbe(t); err != nil {
			return err
		}
	}
	positive, err := settledNetwork(c.http, fixture, baseline)
	if err != nil {
		return err
	}
	if err = compareNetwork(baseline, positive, fixture.Targets, true); err != nil {
		return err
	}
	targets, _ := json.Marshal(fixture.Targets)
	none := fmt.Sprintf(`const net=require('net'),dgram=require('dgram');const targets=%s;
async function probe(t){return new Promise(resolve=>{let s;const done=()=>{if(s){try{s.close?s.close():s.destroy()}catch{}}resolve()};
 try{if(t.Protocol==='udp'||t.Protocol==='dns'){s=dgram.createSocket(t.Address.includes(':')?'udp6':'udp4');s.on('error',done);s.send(Buffer.from('NODE_NONE'),t.Port,t.Address,done);}else{s=net.connect({host:t.Address,port:t.Port});s.on('error',done);s.on('connect',()=>{s.write('NODE_NONE');done()});s.setTimeout(200,done);}}catch{done()}
});}
(async()=>{for(const t of targets)await probe(t);console.log('NONE_ATTEMPTED')})().catch(e=>{console.error(e);process.exitCode=1});`, targets)
	result, err := c.run(c.prepare("node-network-none", "nodejs", none, network.NetworkScope{Mode: "none"}))
	if err != nil {
		return err
	}
	if result.Status != "succeeded" || !strings.Contains(result.Stdout, "NONE_ATTEMPTED") {
		return fmt.Errorf("Node none probes failed to execute: %+v", result)
	}
	afterNone, err := settledNetwork(c.http, fixture, baseline)
	if err != nil {
		return err
	}
	if err = compareNetwork(positive, afterNone, fixture.Targets, false); err != nil {
		return err
	}
	scope := func(t NetworkTarget) network.NetworkScope {
		return network.NetworkScope{Mode: "scoped", Destinations: []network.Destination{{Protocol: t.Protocol, Host: t.Host, Port: t.Port, Addresses: []string{t.Address}}}, MaxEgressBytes: 65536, ExpiresAt: time.Now().Add(time.Duration(c.profile.Limits.WallMillis+5000) * time.Millisecond).Unix()}
	}
	code := func(body string) string {
		return nodeProxyClient + "\n(async()=>{" + body + "})().catch(e=>{console.error(e);process.exitCode=1});"
	}
	primary := NetworkTarget{}
	for i, t := range fixture.Targets {
		if t.Protocol != "http" && t.Protocol != "https" {
			continue
		}
		if t.Protocol == "http" && net.ParseIP(t.Address).To4() != nil {
			primary = t
		}
		beforeAllowed, err := collectNetwork(c.http, fixture)
		if err != nil {
			return err
		}
		script := code("assert.strictEqual(await request(" + strconv.Quote(t.url("/ok")) + "),200);console.log('NODE_SCOPED_OK');")
		result, err = c.run(c.prepare(fmt.Sprintf("node-network-allowed-%d", i), "nodejs", script, scope(t)))
		if err != nil {
			return err
		}
		if result.Status != "succeeded" || !strings.Contains(result.Stdout, "NODE_SCOPED_OK") || result.NetworkReceipt == nil || result.NetworkReceipt.EgressBytes <= 0 {
			return fmt.Errorf("Node proxy HTTP/CONNECT positive failed: %+v", result)
		}
		afterAllowed, err := settledNetwork(c.http, fixture, baseline)
		if err != nil {
			return err
		}
		if err = compareNetwork(beforeAllowed, afterAllowed, []NetworkTarget{t}, true); err != nil {
			return err
		}
	}
	deniedBaseline, err := settledNetwork(c.http, fixture, baseline)
	if err != nil {
		return err
	}
	for i, t := range fixture.AlternateTargets {
		script := code("assert.strictEqual(await request(" + strconv.Quote(primary.url("/ok")) + "),200);assert.strictEqual(await request(" + strconv.Quote(t.url("/ok")) + "),403);console.log('NODE_DENIED');")
		result, err = c.run(c.prepare(fmt.Sprintf("node-network-denied-%d", i), "nodejs", script, scope(primary)))
		if err != nil {
			return err
		}
		if result.Status != "succeeded" || !strings.Contains(result.Stdout, "NODE_DENIED") {
			return fmt.Errorf("Node alternate denial failed: %+v", result)
		}
	}
	afterDenied, err := settledNetwork(c.http, fixture, baseline)
	if err != nil {
		return err
	}
	if err = compareNetwork(deniedBaseline, afterDenied, fixture.AlternateTargets, false); err != nil {
		return err
	}
	beforeQuota, err := collectNetwork(c.http, fixture)
	if err != nil {
		return err
	}
	quota := scope(primary)
	quota.MaxEgressBytes = 128
	script := code("console.log('QUOTA_ATTEMPT_4096');try{await request(" + strconv.Quote(primary.url("/ok")) + ",'x'.repeat(4096))}catch{}console.log('QUOTA_EXERCISED');")
	result, err = c.run(c.prepare("node-network-quota", "nodejs", script, quota))
	if err != nil {
		return err
	}
	afterQuota, err := settledNetwork(c.http, fixture, baseline)
	if err != nil {
		return err
	}
	if err = quotaEvidence(result, beforeQuota, afterQuota, primary, 128); err != nil {
		return err
	}
	// A real live hold distinguishes expiry/cancellation from startup failure.
	for _, mode := range []string{"expiry", "cancel"} {
		policy := scope(primary)
		if mode == "expiry" {
			policy.ExpiresAt = time.Now().Add(3 * time.Second).Unix()
		}
		request := c.prepare("node-network-"+mode, "nodejs", code("await request("+strconv.Quote(primary.url("/hold"))+",'',true);"), policy)
		started := time.Now()
		expiresAfter := time.Unix(policy.ExpiresAt, 0).Sub(started)
		type outcome struct {
			result isolation.ExecutionResult
			err    error
		}
		done := make(chan outcome, 1)
		go func() { r, e := c.run(request); done <- outcome{r, e} }()
		readyCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		readyErr := waitReady(readyCtx, observations, request.Options.ExecutionID)
		stop()
		snapshot, snapshotErr := collectNetwork(c.http, fixture)
		live := snapshotErr == nil && snapshot.BootID == baseline.BootID && snapshot.Active[primary.key()] > baseline.Active[primary.key()]
		liveAt := time.Since(started)
		if mode == "cancel" && readyErr == nil && live {
			err = c.cancel(request)
		}
		completed := <-done
		if completed.err != nil {
			return completed.err
		}
		if err != nil {
			return err
		}
		if readyErr != nil || !live {
			return fmt.Errorf("Node %s had no live ready hold", mode)
		}
		if _, err = settledNetwork(c.http, fixture, baseline); err != nil {
			return err
		}
		if mode == "cancel" {
			if completed.result.Status != "cancelled" {
				return fmt.Errorf("Node cancellation did not terminate")
			}
		} else {
			allowance := 2*time.Duration(c.profile.Limits.CleanupMillis)*time.Millisecond + 250*time.Millisecond
			if err = expiryEvidence(completed.result, true, true, liveAt, time.Since(started), expiresAfter, allowance, time.Duration(c.profile.Limits.WallMillis)*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return nil
}
