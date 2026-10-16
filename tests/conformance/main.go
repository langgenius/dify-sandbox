package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/langgenius/dify-sandbox/tests/conformance/cases"
)

func main() {
	name := flag.String("case", "", "required enumerated conformance group or all")
	endpoint := flag.String("endpoint", "http://candidate-service:8194", "actual candidate endpoint")
	ordinary := flag.String("ordinary-endpoint", "", "separate unchanged ordinary endpoint")
	profile := flag.String("profile", "/qualification/profile.json", "protected profile fixture")
	observations := flag.String("observations", "/observations", "independent supervisor evidence")
	output := flag.String("output", "", "JSON case report path")
	junit := flag.String("junit", "", "JUnit report path")
	measure := flag.Bool("measure", false, "measure actual serial/four-way native workloads")
	flag.Parse()
	if *output != "" {
		if err := os.Setenv("CS5_ARTIFACTS", filepath.Dir(*output)); err != nil {
			fatal(err)
		}
	}
	if *measure {
		if *output == "" {
			fatal(fmt.Errorf("measurement output required"))
		}
		fatal(cases.Measure(*endpoint, *profile, *observations, *output))
		return
	}
	groups := []cases.Group{
		{Name: "roots-and-budgets", Languages: []string{"python3", "nodejs"}, Run: func() error { return cases.RootsAndBudgets(*endpoint, *profile, *observations) }},
		{Name: "lifecycle", Languages: []string{"python3", "nodejs"}, Run: func() error { return cases.Lifecycle(*endpoint, *profile, *observations) }},
		{Name: "dependencies", Languages: []string{"python3", "nodejs"}, Run: func() error { return cases.Dependencies(*endpoint, *profile, *observations) }},
		{Name: "network", Languages: []string{"python3", "nodejs"}, Run: func() error {
			if err := cases.Network(*endpoint, *profile, *observations); err != nil {
				return err
			}
			return cases.NetworkNode(*endpoint, *profile, *observations)
		}},
		{Name: "qualification", Languages: []string{"python3", "nodejs"}, Run: func() error { return cases.Qualification(*endpoint, *profile, *observations) }},
		{Name: "ordinary-regression", Languages: []string{"python3", "nodejs"}, Run: func() error { return cases.OrdinaryRegression(*ordinary, *profile) }},
	}
	if *name != "all" {
		for _, g := range groups {
			if g.Name == *name {
				fatal(g.Run())
				fmt.Println(*name + ": passed")
				return
			}
		}
		fatal(fmt.Errorf("unknown or missing case %q; required: %v or all", *name, cases.RequiredGroups))
		return
	}
	report, err := cases.RunGroups(runtime.GOARCH, groups)
	if *output != "" {
		if e := cases.WriteNetworkEvidence(filepath.Join(filepath.Dir(*output), "network-receipts.json")); e != nil {
			fatal(e)
		}
		if e := cases.WriteReport(*output, report); e != nil {
			fatal(e)
		}
	}
	if *junit != "" {
		if e := cases.WriteJUnit(*junit, report); e != nil {
			fatal(e)
		}
	}
	fatal(err)
	fmt.Println("all: passed")
}
func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
