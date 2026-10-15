package cases

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
)

type endpointClient struct {
	endpoint, observations string
	profile                isolation.Profile
	http                   *http.Client
}

func shaPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
func newEndpoint(endpoint, profilePath, observations string) (*endpointClient, error) {
	raw, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, fmt.Errorf("qualified candidate profile required: %w", err)
	}
	var p isolation.Profile
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if err = p.Validate(); err != nil {
		return nil, err
	}
	return &endpointClient{endpoint: endpoint, observations: observations, profile: p, http: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Duration(p.Limits.WallMillis+p.Limits.QueueMillis+2*p.Limits.CleanupMillis+5000) * time.Millisecond}}, nil
}
func (c *endpointClient) prepare(id, language, code string, scope network.NetworkScope) isolation.InvocationRequest {
	payload, _ := json.Marshal(isolation.Payload{Code: code})
	if scope.Mode == "scoped" {
		sum := shaPayload(payload)
		scope.InputManifestDigest = sum
		scope.ScopeDigest = network.ScopeDigest(scope)
	}
	spec := isolation.InvocationSpec{Version: 1, ExecutionID: "cs1-" + id, Language: language, EnvironmentDigest: c.profile.EnvironmentDigest, ProfileDigest: c.profile.Digest, BudgetReservationRef: "cs1-synthetic", Budget: c.profile.Limits, Network: scope}
	spec.InvocationDigest = isolation.InvocationDigest(spec, payload)
	return isolation.InvocationRequest{Options: spec, Code: code, Language: language}
}
func (c *endpointClient) run(request isolation.InvocationRequest) (isolation.ExecutionResult, error) {
	raw, _ := json.Marshal(request)
	req, _ := http.NewRequest("POST", c.endpoint+"/v1/sandbox/run", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
	response, err := c.http.Do(req)
	if err != nil {
		return isolation.ExecutionResult{}, err
	}
	defer response.Body.Close()
	var result isolation.ExecutionResult
	if err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&result); err != nil {
		return result, err
	}
	if response.StatusCode != 200 {
		return result, fmt.Errorf("candidate HTTP%d: %s", response.StatusCode, result.FailureReason)
	}
	evidence, err := readEvidence(c.observations, request.Options.ExecutionID)
	if err != nil {
		return result, err
	}
	if evidence.Phase != "completed" || !evidence.RootRemoved || !evidence.CgroupRemoved || result.CleanupState != "clean" {
		return result, fmt.Errorf("missing independent clean completion: %s", request.Options.ExecutionID)
	}
	return result, nil
}
func (c *endpointClient) cancel(request isolation.InvocationRequest) error {
	req, _ := http.NewRequest("POST", c.endpoint+"/v1/sandbox/executions/"+request.Options.ExecutionID+"/cancel", nil)
	req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
	req.Header.Set("X-Invocation-Digest", request.Options.InvocationDigest)
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("cancel HTTP%d", response.StatusCode)
	}
	return nil
}
func Qualification(endpoint, profilePath, observations string) error {
	c, err := newEndpoint(endpoint, profilePath, observations)
	if err != nil {
		return err
	}
	for _, language := range []string{"python3", "nodejs"} {
		for _, mutation := range []string{"environment", "profile", "missing-options"} {
			code := "print('MUST_NOT_DISPATCH')"
			if language == "nodejs" {
				code = "console.log('MUST_NOT_DISPATCH')"
			}
			request := c.prepare("qualification-"+language+"-"+mutation, language, code, network.NetworkScope{Mode: "none"})
			if mutation == "environment" {
				request.Options.EnvironmentDigest = strings.Repeat("0", 64)
			}
			if mutation == "profile" {
				request.Options.ProfileDigest = strings.Repeat("0", 64)
			}
			payload, _ := json.Marshal(isolation.Payload{Code: code})
			request.Options.InvocationDigest = isolation.InvocationDigest(request.Options, payload)
			raw, _ := json.Marshal(request)
			if mutation == "missing-options" {
				raw, _ = json.Marshal(map[string]interface{}{"language": language, "code": code, "enable_network": true})
			}
			req, _ := http.NewRequest("POST", endpoint+"/v1/sandbox/run", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
			response, err := c.http.Do(req)
			if err != nil {
				return err
			}
			body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			if response.StatusCode < 400 || strings.Contains(string(body), "MUST_NOT_DISPATCH") {
				return fmt.Errorf("qualification %s/%s did not reject before dispatch: HTTP%d", language, mutation, response.StatusCode)
			}
			if _, err = os.Stat(filepath.Join(observations, request.Options.ExecutionID+".json")); !os.IsNotExist(err) {
				return fmt.Errorf("invalid request produced execution evidence")
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(profilePath), "dependency-drift.json"))
	if err != nil {
		return fmt.Errorf("actual-image dependency drift evidence required: %w", err)
	}
	var drift struct {
		EnvironmentDigest string            `json:"environment_digest"`
		Cases             map[string]string `json:"cases"`
	}
	if err = json.Unmarshal(raw, &drift); err != nil {
		return err
	}
	if drift.EnvironmentDigest != c.profile.EnvironmentDigest {
		return fmt.Errorf("dependency drift environment mismatch")
	}
	required := []string{"usr/bin/python3", "usr/bin/node", "usr/local/lib/python3.12/site-packages/requests/__init__.py", "opt/node/node_modules/koffi/index.js", "var/sandbox/sandbox-python/python.so", "var/sandbox/sandbox-nodejs/nodejs.so", "usr/lib/ssl/cert.pem"}
	if len(drift.Cases) != len(required) {
		return fmt.Errorf("dependency drift inventory mismatch")
	}
	for _, id := range required {
		if drift.Cases[id] != "rejected-and-restored" {
			return fmt.Errorf("dependency drift not exercised: %s", id)
		}
	}
	return nil
}
func OrdinaryRegression(endpoint, profilePath string) error {
	if endpoint == "" {
		return fmt.Errorf("separate ordinary endpoint required")
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
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 20 * time.Second}
	before, err := collectNetwork(client, fixture)
	if err != nil {
		return err
	}
	target := fixture.Targets[0]
	for _, t := range fixture.Targets {
		if t.Protocol == "http" && strings.Contains(t.Address, ".") {
			target = t
			break
		}
	}
	for _, language := range []string{"python3", "nodejs"} {
		for _, enabled := range []bool{false, true} {
			code := "import json\nfrom jinja2 import Environment\nassert Environment().from_string('{{x|upper}}').render(x='ok')=='OK'\nprint('ORDINARY_OK')"
			if language == "nodejs" {
				code = "const assert=require('assert'),koffi=require('koffi');assert.strictEqual(koffi.load('libc.so.6').func('int abs(int)')(-42),42);console.log('ORDINARY_OK');"
			}
			if enabled {
				if language == "python3" {
					code = "import requests\nassert requests.get(" + fmt.Sprintf("%q", target.url("/ok")) + ",timeout=3).status_code==200\nprint('ORDINARY_OK')"
				} else {
					code = "require('http').get(" + fmt.Sprintf("%q", target.url("/ok")) + ",r=>{if(r.statusCode!==200)throw Error('HTTP');r.resume();r.on('end',()=>console.log('ORDINARY_OK'))}).on('error',e=>{console.error(e);process.exitCode=1});"
				}
			}
			payload, _ := json.Marshal(map[string]interface{}{"language": language, "code": code, "preload": "", "enable_network": enabled})
			req, _ := http.NewRequest("POST", endpoint+"/v1/sandbox/run", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Api-Key", os.Getenv("SANDBOX_API_KEY"))
			response, err := client.Do(req)
			if err != nil {
				return err
			}
			var result struct {
				Code int `json:"code"`
				Data struct {
					Stdout string `json:"stdout"`
					Error  string `json:"error"`
				} `json:"data"`
			}
			err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result)
			response.Body.Close()
			if err != nil {
				return err
			}
			if response.StatusCode != 200 || result.Code != 0 || result.Data.Error != "" || strings.TrimSpace(result.Data.Stdout) != "ORDINARY_OK" {
				return fmt.Errorf("ordinary %s network=%v failed: HTTP%d %+v", language, enabled, response.StatusCode, result)
			}
		}
	}
	after, err := settledNetwork(client, fixture, before)
	if err != nil {
		return err
	}
	return compareNetwork(before, after, []NetworkTarget{target}, true)
}
func waitEndpoint(ctx context.Context, endpoint string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	for {
		request, _ := http.NewRequestWithContext(ctx, "GET", endpoint+"/health", nil)
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
