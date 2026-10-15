// This synthetic candidate is only a conformance fixture. Production must
// inject its real reservation resolver and environment qualification owner.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/controller"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/tests/conformance/cases"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	probe := flag.Bool("lifecycle-reconcile", false, "run actual restart/quarantine probe before serving")
	orphan := flag.Bool("lifecycle-orphan", false, "private restart child")
	flag.Parse()
	if err := static.InitConfig("/qualification/server.yaml"); err != nil {
		log.Fatal("authenticated candidate config required: ", err)
	}
	raw, err := os.ReadFile("/qualification/profile.json")
	if err != nil {
		log.Fatal("qualified candidate profile missing: ", err)
	}
	var p isolation.Profile
	if err = json.Unmarshal(raw, &p); err != nil {
		log.Fatal(err)
	}
	if p.EnvironmentRoot != "/environment-root" {
		log.Fatal("candidate requires baked environment root")
	}
	recorder := cases.NewObservationRecorder("/observations", p.MaxExecutions)
	observer := func(o isolation.Observation) {
		if err := recorder.Observe(o); err != nil {
			log.Fatal("conformance observation failed: ", err)
		}
	}
	resolver := func(_ context.Context, s isolation.InvocationSpec) (isolation.Budget, error) {
		if !strings.HasPrefix(s.ExecutionID, "cs1-") || s.BudgetReservationRef != "cs1-synthetic" {
			return isolation.Budget{}, fmt.Errorf("not a synthetic conformance reservation")
		}
		return p.Limits, nil
	}
	factory := func(s isolation.InvocationSpec, _ isolation.Payload) (isolation.Launch, error) {
		switch s.Language {
		case "python3":
			return service.PythonIsolatedLaunch("/usr/bin/python3"), nil
		case "nodejs":
			return service.NodeIsolatedLaunch("/usr/bin/node"), nil
		default:
			return isolation.Launch{}, fmt.Errorf("unsupported language")
		}
	}
	if *orphan {
		if err := orphanProbe(p, resolver, factory); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *probe {
		if err := reconcileProbe(p, resolver, factory); err != nil {
			log.Fatal(err)
		}
	}
	supervisor, err := isolation.NewSupervisor(p, resolver, factory, observer)
	if err != nil {
		log.Fatal(err)
	}
	router := gin.New()
	controller.SetupBuilder(router, supervisor)
	server := &http.Server{Addr: ":8194", Handler: router, ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: time.Duration(p.Limits.WallMillis+p.Limits.QueueMillis+p.Limits.CleanupMillis+2000) * time.Millisecond}
	log.Fatal(server.ListenAndServe())
}
