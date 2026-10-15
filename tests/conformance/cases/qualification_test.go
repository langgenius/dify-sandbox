package cases

import (
	"errors"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"strings"
	"testing"
)

func TestInventoryRequiresAllSixGroupsAndBothLanguages(t *testing.T) {
	groups := []Group{}
	for _, name := range RequiredGroups {
		groups = append(groups, Group{Name: name, Languages: []string{"python3", "nodejs"}, Run: func() error { return nil }})
	}
	ids, err := CaseInventory("arm64", groups)
	if err != nil || len(ids) != 12 {
		t.Fatalf("inventory: %v %v", ids, err)
	}
	for _, bad := range [][]Group{groups[:5], append(groups, groups[0])} {
		if _, err := CaseInventory("arm64", bad); err == nil {
			t.Fatal("accepted missing/duplicate groups")
		}
	}
	groups[0].Languages = []string{"python3", "python3"}
	if _, err := CaseInventory("arm64", groups); err == nil {
		t.Fatal("accepted duplicate language")
	}
}

func TestReportContainsObservedFailuresAndNoImplicitPass(t *testing.T) {
	groups := []Group{}
	for _, name := range RequiredGroups {
		groups = append(groups, Group{Name: name, Languages: []string{"python3", "nodejs"}, Run: func() error { return nil }})
	}
	groups[3].Run = func() error { return errors.New("canary positive control absent") }
	report, err := RunGroups("arm64", groups)
	if err == nil || !strings.Contains(err.Error(), "network") {
		t.Fatalf("missing observed failure: %v", err)
	}
	if len(report.Cases) != 12 || report.Failed != 2 || report.Skipped != 0 {
		t.Fatalf("bad report: %+v", report)
	}
	for _, result := range report.Cases {
		if strings.HasSuffix(result.CaseID, "/network") && (result.Status != "failed" || !strings.Contains(result.Error, "positive")) {
			t.Fatalf("failure discarded: %+v", result)
		}
	}
}

func TestNoneCustodyRejectsStaticHostUIDAndMissingUserNamespace(t *testing.T) {
	a := Evidence{Observation: isolation.Observation{HostUID: 65537, Namespaces: map[string]string{"user": "user:[100]"}}}
	b := Evidence{Observation: isolation.Observation{HostUID: 65537, Namespaces: map[string]string{"user": "user:[101]"}}}
	if RequireDistinctCustody(a, b) == nil {
		t.Fatal("shared static host UID qualified")
	}
	b.HostUID = 20001
	a.HostUID = 20000
	a.Controls = map[string]string{"uid_map": "65537 20000 1"}
	b.Controls = map[string]string{"uid_map": "65537 20001 1"}
	if err := RequireDistinctCustody(a, b); err != nil {
		t.Fatal(err)
	}
	delete(b.Namespaces, "user")
	if RequireDistinctCustody(a, b) == nil {
		t.Fatal("missing user namespace qualified")
	}
}

func TestLifecycleCustodyRequiresReleasedLeaseReuseAndQuarantineSeparation(t *testing.T) {
	a := Evidence{Observation: isolation.Observation{HostUID: 20000, Namespaces: map[string]string{"user": "user:[100]"}, Controls: map[string]string{"uid_map": "65537 20000 1"}}}
	b := Evidence{Observation: isolation.Observation{HostUID: 20001, Namespaces: map[string]string{"user": "user:[101]"}, Controls: map[string]string{"uid_map": "65537 20001 1"}}}
	if err := RequireLifecycleCustody("20000", a, b); err != nil {
		t.Fatal(err)
	}
	if RequireLifecycleCustody("65537", a, b) == nil {
		t.Fatal("child UID mistaken for host custody")
	}
	b.HostUID = a.HostUID
	if RequireLifecycleCustody("20000", a, b) == nil {
		t.Fatal("quarantined UID reused")
	}
}
