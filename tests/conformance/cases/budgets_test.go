package cases

import "testing"

func TestMeasuredProfileNeedsEveryLanguageAndConcurrencyCohort(t *testing.T) {
	if _, err := SelectBudget(nil); err == nil {
		t.Fatal("missing measurements accepted")
	}
	rows := []Measurement{}
	for _, language := range []string{"python3", "nodejs", "jinja2"} {
		for _, mode := range []string{"serial", "four-way"} {
			rows = append(rows, Measurement{Language: language, Mode: mode, SampleCount: 3, SampleIntervalMicros: 1000, Source: "supervisor-cgroup-proc", ColdStart: true, MemoryBytes: 10000000, CPUTimeMillis: 100, PIDs: 8, FDs: 20, DiskBytes: 5000, StdoutBytes: 100, StderrBytes: 0, WallMillis: 1000})
		}
	}
	budget, err := SelectBudget(rows)
	if err != nil {
		t.Fatal(err)
	}
	if budget.MemoryBytes <= 10000000 || budget.CPUTimeMillis <= 100 || budget.FDs <= 20 || budget.Concurrent < 4 {
		t.Fatalf("headroom omitted: %+v", budget)
	}
	if _, err := SelectBudget(rows[:5]); err == nil {
		t.Fatal("missing cohort accepted")
	}
	rows[0].Source = "user-stdout"
	if _, err := SelectBudget(rows); err == nil {
		t.Fatal("untrusted measurements accepted")
	}
}

func TestResourceEvidenceRejectsSetupOnlyAndPayloadCapabilities(t *testing.T) {
	sample := nativeSample()
	sample.NativeSamples = 0
	if RequireNativeResourceSample(sample) == nil {
		t.Fatal("setup-only samples qualified native execution")
	}
	sample.NativeSamples = 2
	if err := RequireNativeResourceSample(sample); err != nil {
		t.Fatal(err)
	}
	sample.NativeCapabilities = 1
	if RequireNativeResourceSample(sample) == nil {
		t.Fatal("payload retained capability")
	}
}
