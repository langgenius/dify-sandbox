package main

import (
	"flag"
	"fmt"
	"github.com/langgenius/dify-sandbox/tests/conformance/cases"
	"os"
)

func main() {
	name := flag.String("case", "", "required enumerated conformance case")
	endpoint := flag.String("endpoint", "http://candidate-service:8194", "candidate HTTP endpoint")
	profile := flag.String("profile", "/qualification/profile.json", "server-owned synthetic profile fixture")
	observations := flag.String("observations", "/observations", "independent supervisor evidence")
	flag.Parse()
	var err error
	switch *name {
	case "dependencies":
		err = cases.Dependencies(*endpoint, *profile, *observations)
	case "lifecycle":
		err = cases.Lifecycle(*endpoint, *profile, *observations)
	case "roots-and-budgets":
		err = cases.RootsAndBudgets(*endpoint, *profile, *observations)
	default:
		err = fmt.Errorf("unknown or missing case %q; required cases: roots-and-budgets, lifecycle, dependencies", *name)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(*name + ": passed")
}
