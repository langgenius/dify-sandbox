package cases

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"time"
)

var RequiredGroups = []string{"roots-and-budgets", "lifecycle", "dependencies", "network", "qualification", "ordinary-regression"}

type Group struct {
	Name      string
	Languages []string
	Run       func() error
}
type CaseResult struct {
	CaseID  string  `json:"case_id"`
	Status  string  `json:"status"`
	Error   string  `json:"error,omitempty"`
	Seconds float64 `json:"seconds"`
}
type Report struct {
	Architecture string       `json:"architecture"`
	Cases        []CaseResult `json:"case_results"`
	CaseIDs      []string     `json:"case_ids"`
	Skipped      int          `json:"skipped"`
	Failed       int          `json:"failed"`
	XFailed      int          `json:"xfailed"`
}

func CaseInventory(architecture string, groups []Group) ([]string, error) {
	if architecture != "amd64" && architecture != "arm64" {
		return nil, fmt.Errorf("native supported architecture required")
	}
	if len(groups) != len(RequiredGroups) {
		return nil, fmt.Errorf("all six nonempty groups required")
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, g := range groups {
		valid := false
		for _, name := range RequiredGroups {
			valid = valid || g.Name == name
		}
		if !valid || seen[g.Name] || g.Run == nil || len(g.Languages) != 2 {
			return nil, fmt.Errorf("invalid, duplicate or empty group %s", g.Name)
		}
		seen[g.Name] = true
		languages := map[string]bool{}
		for _, l := range g.Languages {
			if (l != "python3" && l != "nodejs") || languages[l] {
				return nil, fmt.Errorf("invalid language inventory")
			}
			languages[l] = true
			ids = append(ids, "linux/"+architecture+"/"+l+"/"+g.Name)
		}
	}
	return ids, nil
}
func RunGroups(architecture string, groups []Group) (Report, error) {
	ids, err := CaseInventory(architecture, groups)
	report := Report{Architecture: architecture, CaseIDs: ids, Cases: []CaseResult{}}
	if err != nil {
		return report, err
	}
	failures := []string{}
	for _, g := range groups {
		start := time.Now()
		err := g.Run()
		elapsed := time.Since(start).Seconds()
		for _, language := range g.Languages {
			result := CaseResult{CaseID: "linux/" + architecture + "/" + language + "/" + g.Name, Status: "passed", Seconds: elapsed}
			if err != nil {
				result.Status = "failed"
				result.Error = err.Error()
				report.Failed++
			}
			report.Cases = append(report.Cases, result)
		}
		if err != nil {
			failures = append(failures, g.Name+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return report, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return report, nil
}
func WriteReport(path string, report Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
func WriteJUnit(path string, report Report) error {
	type failure struct {
		Message string `xml:"message,attr"`
		Text    string `xml:",chardata"`
	}
	type testcase struct {
		Name    string   `xml:"name,attr"`
		Time    float64  `xml:"time,attr"`
		Failure *failure `xml:"failure,omitempty"`
	}
	suite := struct {
		XMLName  xml.Name   `xml:"testsuite"`
		Tests    int        `xml:"tests,attr"`
		Failures int        `xml:"failures,attr"`
		Cases    []testcase `xml:"testcase"`
	}{Tests: len(report.Cases), Failures: report.Failed}
	for _, r := range report.Cases {
		c := testcase{Name: r.CaseID, Time: r.Seconds}
		if r.Status != "passed" {
			c.Failure = &failure{Message: r.Status, Text: r.Error}
		}
		suite.Cases = append(suite.Cases, c)
	}
	data, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

// RequireDistinctCustody applies to none and scoped workloads alike. A private
// PID/network namespace does not establish a distinct host UID lease.
func RequireDistinctCustody(a, b Evidence) error {
	if a.HostUID < 11000 || b.HostUID < 11000 || a.HostUID == 65537 || b.HostUID == 65537 || a.HostUID == b.HostUID {
		return fmt.Errorf("concurrent requests lack distinct owned host UID leases")
	}
	if a.Namespaces["user"] == "" || b.Namespaces["user"] == "" || a.Namespaces["user"] == b.Namespaces["user"] {
		return fmt.Errorf("concurrent requests lack distinct user namespaces")
	}
	for _, e := range []Evidence{a, b} {
		mapped := false
		for _, line := range strings.Split(e.Controls["uid_map"], "\n") {
			fields := strings.Fields(line)
			if len(fields) == 3 && fields[0] == "65537" && fields[1] == fmt.Sprint(e.HostUID) && fields[2] == "1" {
				mapped = true
			}
		}
		if !mapped {
			return fmt.Errorf("host UID is not corroborated by actual uid_map")
		}
	}
	return nil
}

func RequireLifecycleCustody(releasedHostUID string, quarantined, peer Evidence) error {
	if err := RequireDistinctCustody(quarantined, peer); err != nil {
		return err
	}
	if fmt.Sprint(quarantined.HostUID) != releasedHostUID {
		return fmt.Errorf("released host UID was not reused by the next owned request")
	}
	return nil
}
