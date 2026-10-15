// This synthetic candidate is only a conformance fixture. Production must
// inject its real reservation resolver and environment qualification owner.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/controller"
	"github.com/langgenius/dify-sandbox/internal/core/runner/isolation"
	"github.com/langgenius/dify-sandbox/internal/service"
	"github.com/langgenius/dify-sandbox/tests/conformance/cases"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	raw, err := os.ReadFile("/qualification/profile.json")
	if err != nil {
		log.Fatal("qualified synthetic profile missing: ", err)
	}
	var p isolation.Profile
	if err = json.Unmarshal(raw, &p); err != nil {
		log.Fatal(err)
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
	supervisor, err := isolation.NewSupervisor(p, resolver, factory, observer)
	if err != nil {
		log.Fatal(err)
	}
	router := gin.New()
	router.POST("/v1/sandbox/run", controller.NewBuilderRunController(supervisor))
	server := &http.Server{Addr: ":8194", Handler: router, ReadHeaderTimeout: time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: time.Duration(p.Limits.WallMillis+p.Limits.QueueMillis+p.Limits.CleanupMillis+2000) * time.Millisecond}
	log.Fatal(server.ListenAndServe())
}
