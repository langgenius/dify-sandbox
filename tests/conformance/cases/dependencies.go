package cases

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Dependencies uses the actual Builder endpoint and requires independent cleanup
// evidence. It must never turn a missing profile or kernel setup into a pass.
func Dependencies(endpoint, profilePath, observations string) error {
	raw, err := os.ReadFile(profilePath)
	if err != nil {
		return fmt.Errorf("qualified candidate profile required: %w", err)
	}
	var p isolation.Profile
	if err = json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if err = p.Validate(); err != nil {
		return err
	}
	client := &http.Client{Timeout: time.Duration(p.Limits.WallMillis+p.Limits.QueueMillis+p.Limits.CleanupMillis+2000) * time.Millisecond}
	run := func(id, language, code string) error {
		spec := isolation.InvocationSpec{Version: 1, ExecutionID: id, Language: language, EnvironmentDigest: p.EnvironmentDigest, ProfileDigest: p.Digest, BudgetReservationRef: "cs1-synthetic", Budget: p.Limits, Network: network.NetworkScope{Mode: "none"}}
		payload, _ := json.Marshal(isolation.Payload{Code: code})
		spec.InvocationDigest = isolation.InvocationDigest(spec, payload)
		body, _ := json.Marshal(isolation.InvocationRequest{Options: spec, Language: language, Code: code})
		req, _ := http.NewRequest("POST", endpoint+"/v1/sandbox/run", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		var result isolation.ExecutionResult
		if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return err
		}
		if resp.StatusCode != 200 || result.Status != "succeeded" || result.CleanupState != "clean" || strings.TrimSpace(result.Stdout) != "DEPENDENCIES_OK" {
			return fmt.Errorf("dependency workload %s failed: HTTP %d %+v", id, resp.StatusCode, result)
		}
		evidence, err := readEvidence(observations, id)
		if err != nil {
			return err
		}
		if evidence.Phase != "completed" || !evidence.RootRemoved || !evidence.CgroupRemoved || len(evidence.Namespaces) < 5 {
			return fmt.Errorf("dependency cleanup/namespaces not proven for %s", id)
		}
		return nil
	}
	python := `import os, json, requests, httpx, socks, ssl
from jinja2 import Environment
assert requests.__version__ == '2.32.3' and httpx.__version__ == '0.27.2'
assert Environment().from_string('{% for x in xs %}{{x|upper}}{% endfor %}').render(xs=['a','é']) == 'AÉ'
assert json.loads(json.dumps({'x':[1,True,None],'text':'你好'})) == {'x':[1,True,None],'text':'你好'}
assert not os.path.exists('/work/tmp/peer')
open('/work/tmp/peer','w').write('private')
for name in ['/usr/local/lib/python3.12/site-packages/requests/__init__.py','/usr/bin/python3']:
    try: open(name,'ab')
    except OSError as e: assert e.errno in (13,30)
    else: raise AssertionError('dependency writable')
assert not os.path.exists('/qualification') and not os.path.exists('/conf')
print('DEPENDENCIES_OK')`
	node := `const fs=require('fs'),assert=require('assert'),koffi=require('koffi');
assert.strictEqual(koffi.version,'2.10.1');
assert.strictEqual(koffi.load('libc.so.6').func('int abs(int)')(-42),42);
assert.deepStrictEqual(JSON.parse(JSON.stringify({x:[1,true,null],text:'你好'})),{x:[1,true,null],text:'你好'});
assert.strictEqual(fs.existsSync('/work/tmp/peer'),false);
fs.writeFileSync('/work/tmp/peer','private');
assert.throws(()=>fs.appendFileSync('/opt/node/node_modules/koffi/index.js',''),/EROFS|EACCES/);
assert.strictEqual(fs.existsSync('/qualification'),false);
console.log('DEPENDENCIES_OK');`
	// Two serial rounds plus cross-language parallel peers expose cache reuse.
	for i := 0; i < 2; i++ {
		if err := run(fmt.Sprintf("cs1-deps-python-%d", i), "python3", python); err != nil {
			return err
		}
		if err := run(fmt.Sprintf("cs1-deps-node-%d", i), "nodejs", node); err != nil {
			return err
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = run("cs1-deps-parallel-python", "python3", python) }()
	go func() { defer wg.Done(); errs[1] = run("cs1-deps-parallel-node", "nodejs", node) }()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	// Builder profile must not expose dependency mutation routes.
	for _, route := range []struct{ method, path string }{{"POST", "update"}, {"GET", "refresh"}} {
		req, _ := http.NewRequest(route.method, endpoint+"/v1/sandbox/dependencies/"+route.path, nil)
		req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			return fmt.Errorf("builder dependency mutation route reachable: %s", route.path)
		}
	}
	return nil
}
