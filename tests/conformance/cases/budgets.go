package cases

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/core/runner/network"
)

type Measurement struct {
	Language             string  `json:"language"`
	Mode                 string  `json:"mode"`
	SampleCount          int64   `json:"sample_count"`
	SampleIntervalMicros int64   `json:"sample_interval_micros"`
	Source               string  `json:"source"`
	ColdStart            bool    `json:"cold_start"`
	MemoryBytes          int64   `json:"memory_bytes"`
	CPUTimeMillis        int64   `json:"cpu_time_millis"`
	PIDs                 int64   `json:"pids"`
	FDs                  int64   `json:"fds"`
	DiskBytes            int64   `json:"disk_bytes"`
	StdoutBytes          int64   `json:"stdout_bytes"`
	StderrBytes          int64   `json:"stderr_bytes"`
	WallMillis           int64   `json:"wall_millis"`
	CompletionMillis     []int64 `json:"completion_millis"`
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func SelectBudget(rows []Measurement) (isolation.Budget, error) {
	var b isolation.Budget
	seen := map[string]bool{}
	for _, r := range rows {
		key := r.Language + "/" + r.Mode
		if seen[key] || r.SampleCount < 3 || r.Source != "supervisor-cgroup-proc" || !r.ColdStart || r.SampleIntervalMicros <= 0 || r.MemoryBytes <= 0 || r.CPUTimeMillis <= 0 || r.PIDs <= 0 || r.FDs <= 0 || r.DiskBytes <= 0 || r.WallMillis <= 0 {
			return b, fmt.Errorf("missing/invalid independent repeated measurement")
		}
		seen[key] = true
		b.MemoryBytes = max64(b.MemoryBytes, r.MemoryBytes)
		b.CPUTimeMillis = max64(b.CPUTimeMillis, r.CPUTimeMillis)
		b.PIDs = max64(b.PIDs, r.PIDs)
		b.FDs = max64(b.FDs, r.FDs)
		b.DiskBytes = max64(b.DiskBytes, r.DiskBytes)
		b.StdoutBytes = max64(b.StdoutBytes, r.StdoutBytes)
		b.StderrBytes = max64(b.StderrBytes, r.StderrBytes)
		b.WallMillis = max64(b.WallMillis, r.WallMillis)
	}
	if len(seen) != 6 {
		return b, fmt.Errorf("six measurement cohorts required")
	}
	for _, l := range []string{"python3", "nodejs", "jinja2"} {
		for _, m := range []string{"serial", "four-way"} {
			if !seen[l+"/"+m] {
				return b, fmt.Errorf("missing measurement %s/%s", l, m)
			}
		}
	}
	// Twice observed peaks, rounded with explicit fixed startup/file headroom.
	b.MemoryBytes = ((2*b.MemoryBytes + (16 << 20) + (1 << 20) - 1) / (1 << 20)) * (1 << 20)
	b.CPUTimeMillis = 2*b.CPUTimeMillis + 250
	b.PIDs = 2*b.PIDs + 8
	b.FDs = 2*b.FDs + 32
	b.DiskBytes = ((2*b.DiskBytes + (1 << 20) + (4096 - 1)) / 4096) * 4096
	b.StdoutBytes = 2*b.StdoutBytes + 65536
	b.StderrBytes = 2*b.StderrBytes + 65536
	b.WallMillis = max64(2*b.WallMillis+5000, 8*b.CPUTimeMillis+6000)
	b.InputBytes = 2 << 20
	b.CodeBytes = 1 << 20
	b.Concurrent = 4
	b.QueueDepth = 8
	b.CPUQuotaMicros = 100000
	b.CPUPeriodMicros = 100000
	b.QueueMillis = b.WallMillis
	b.CleanupMillis = 3000
	if err := b.Validate(); err != nil {
		return b, err
	}
	return b, nil
}
func Measure(endpoint, profilePath, observations, output string) error {
	c, err := newEndpoint(endpoint, profilePath, observations)
	if err != nil {
		return err
	}
	if c.profile.MaxConcurrent < 4 || c.profile.Limits.Concurrent < 4 {
		return fmt.Errorf("measurement requires four actual concurrent slots")
	}
	workloads := map[string]string{
		"python3": `import json,time,requests,httpx,socks,ssl
from jinja2 import Environment
assert requests.__version__=='2.32.3'
assert json.loads(json.dumps({'items':list(range(10000))}))['items'][9999]==9999
assert Environment().from_string('{{x|upper}}').render(x='useful')=='USEFUL'
open('/work/measurement','wb').write(b'x'*32768)
print('CS1_READY',flush=True)
time.sleep(.1)
print('MEASURE_OK')`,
		"nodejs": `const fs=require('fs'),assert=require('assert'),koffi=require('koffi');assert.strictEqual(koffi.load('libc.so.6').func('int abs(int)')(-42),42);assert.strictEqual(JSON.parse(JSON.stringify(Array.from({length:10000},(_,i)=>i)))[9999],9999);fs.writeFileSync('/work/measurement',Buffer.alloc(32768));console.log('CS1_READY');setTimeout(()=>console.log('MEASURE_OK'),100);`,
		"jinja2": `import time
from jinja2 import Environment
result=Environment().from_string('{% for x in xs %}{{x}}{% endfor %}').render(xs=list(range(1000)))
assert result.startswith('0123456789')
open('/work/measurement','wb').write(result.encode())
print('CS1_READY',flush=True)
time.sleep(.1)
print('MEASURE_OK')`,
	}
	rows := map[string]*Measurement{}
	var mu sync.Mutex
	run := func(language, mode string, round, slot int) error {
		native := language
		if native == "jinja2" {
			native = "python3"
		}
		request := c.prepare(fmt.Sprintf("measure-%s-%s-%d-%d", language, mode, round, slot), native, workloads[language], network.NetworkScope{Mode: "none"})
		result, err := c.run(request)
		if err != nil {
			return err
		}
		if result.Status != "succeeded" || workloadOutput(result.Stdout) != "MEASURE_OK\n" {
			return fmt.Errorf("normal useful workload failed: %+v", result)
		}
		raw, err := os.ReadFile(filepath.Join(observations, request.Options.ExecutionID+".resources.json"))
		if err != nil {
			return err
		}
		var sample ResourceSample
		if err = json.Unmarshal(raw, &sample); err != nil {
			return err
		}
		if err := RequireNativeResourceSample(sample); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		key := language + "/" + mode
		r := rows[key]
		if r == nil {
			r = &Measurement{Language: language, Mode: mode, Source: "supervisor-cgroup-proc", ColdStart: true, SampleIntervalMicros: sample.IntervalMicros}
			rows[key] = r
		}
		r.SampleCount++
		r.MemoryBytes = max64(r.MemoryBytes, result.Usage.MemoryPeakBytes)
		r.CPUTimeMillis = max64(r.CPUTimeMillis, (result.Usage.CPUTimeMicros+999)/1000)
		r.PIDs = max64(r.PIDs, sample.PIDs)
		r.FDs = max64(r.FDs, sample.FDs)
		r.DiskBytes = max64(r.DiskBytes, sample.DiskBytes)
		r.StdoutBytes = max64(r.StdoutBytes, int64(len(result.Stdout)))
		r.StderrBytes = max64(r.StderrBytes, int64(len(result.Stderr)))
		r.WallMillis = max64(r.WallMillis, result.Usage.WallMillis)
		r.CompletionMillis = append(r.CompletionMillis, result.Usage.WallMillis)
		return nil
	}
	for round := 0; round < 3; round++ {
		for _, language := range []string{"python3", "nodejs", "jinja2"} {
			if err = run(language, "serial", round, 0); err != nil {
				return err
			}
		}
		languages := []string{"python3", "nodejs", "jinja2", "python3"}
		if round%2 == 1 {
			languages[3] = "nodejs"
		}
		var wg sync.WaitGroup
		errs := make([]error, 4)
		for i, language := range languages {
			wg.Add(1)
			go func(i int, l string) { defer wg.Done(); errs[i] = run(l, "four-way", round, i) }(i, language)
		}
		wg.Wait()
		for _, err = range errs {
			if err != nil {
				return err
			}
		}
	}
	ordered := []Measurement{}
	for _, l := range []string{"python3", "nodejs", "jinja2"} {
		for _, m := range []string{"serial", "four-way"} {
			ordered = append(ordered, *rows[l+"/"+m])
		}
	}
	budget, err := SelectBudget(ordered)
	if err != nil {
		return err
	}
	artifact := struct {
		Budget       isolation.Budget `json:"budget"`
		Measurements []Measurement    `json:"measurements"`
		Headroom     string           `json:"headroom"`
	}{budget, ordered, "2x observed peaks plus documented startup/file headroom; FD/disk/PID are 2ms sampled lower bounds, memory.peak and cpu.stat are kernel counters; rerun limit/recovery cases before release"}
	raw, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, append(raw, '\n'), 0600)
}
