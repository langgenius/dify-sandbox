package cases

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func stringList(t *testing.T, path, owner string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var result []string
	ast.Inspect(f, func(n ast.Node) bool {
		var body ast.Node
		switch v := n.(type) {
		case *ast.FuncDecl:
			if v.Name.Name == owner {
				body = v.Body
			}
		case *ast.ValueSpec:
			if len(v.Names) == 1 && v.Names[0].Name == owner {
				body = v.Values[0]
			}
		}
		if body != nil {
			ast.Inspect(body, func(n ast.Node) bool {
				c, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				typ, ok := c.Type.(*ast.ArrayType)
				if !ok {
					return true
				}
				id, ok := typ.Elt.(*ast.Ident)
				if !ok || id.Name != "string" {
					return true
				}
				for _, e := range c.Elts {
					v, ok := e.(*ast.BasicLit)
					if !ok {
						t.Fatal("nonliteral native filesystem policy")
					}
					s, err := strconv.Unquote(v.Value)
					if err != nil {
						t.Fatal(err)
					}
					result = append(result, s)
				}
				return false
			})
			return false
		}
		return true
	})
	if len(result) == 0 {
		t.Fatal("native policy inventory absent", path, owner)
	}
	return result
}
func TestDiagnosticFilterMatchesUnchangedNativeOwners(t *testing.T) {
	for _, language := range []string{"python", "nodejs"} {
		name := "pythonFilesystem"
		if language == "nodejs" {
			name = "nodeFilesystem"
		}
		actual := stringList(t, "../../../internal/core/lib/"+language+"/add_seccomp.go", "isolatedFilesystemSyscalls")
		fixture := stringList(t, "../probe-filter/main.go", name)
		if !reflect.DeepEqual(actual, fixture) {
			t.Fatal("diagnostic policy differs from native owner", language, actual, fixture)
		}
		for _, arch := range []string{"amd64", "arm64"} {
			raw, err := os.ReadFile("../../../internal/static/" + language + "_syscall/syscalls_" + arch + ".go")
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{"syscall.SYS_TGKILL", "syscall.SYS_GETTID", "syscall.SYS_GETPID"} {
				if !strings.Contains(string(raw), required) {
					t.Fatal(language, arch, "missing native binding", required)
				}
			}
			for _, forbidden := range []string{"syscall.SYS_KILL,", "syscall.SYS_CHMOD,", "syscall.SYS_FCHMODAT,"} {
				if strings.Contains(string(raw), forbidden) {
					t.Fatal("unexpected native policy expansion", language, arch, forbidden)
				}
			}
		}
	}
}
func TestGeneratedIsolationProbesUseNativeAllowedOperations(t *testing.T) {
	bundle := map[string]map[string]string{}
	for _, language := range []string{"python3", "nodejs"} {
		for _, mode := range []string{"none", "scoped"} {
			victim := victimCode(language, "cs1-custody-victim-"+mode+"-"+language)
			attacker := attackerCode(language, []string{"/proc/2147483000/root/work/custody-private", "/unmounted-peer/custody-private", "/synthetic-host/sentinel", "/synthetic-control/sentinel"}, ProcessIdentity{PID: 2147483000, TGID: 2147483000, StartTime: "CS5_COMPONENT_START", UserNS: "CS5_COMPONENT_USERNS", PIDNS: "CS5_COMPONENT_PIDNS"})
			bundle[language+"/"+mode] = map[string]string{"victim": victim, "attacker": attacker}
			for _, forbidden := range []string{"os.chmod(", "os.kill(", "process.kill(", "require('os')"} {
				if strings.Contains(victim, forbidden) || strings.Contains(attacker, forbidden) {
					t.Error("exact generated probe calls native-killed operation", language, forbidden)
				}
			}
		}
	}
	// Optional diagnostic export does not claim execution or native qualification.
	if dir := os.Getenv("CS5_PROBE_BUNDLE"); dir != "" {
		raw, err := json.Marshal(bundle)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "generated.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
