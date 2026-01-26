package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/controller"
	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/middleware"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/telemetry"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func initConfig() {
	// auto migrate database
	err := static.InitConfig("conf/config.yaml")
	if err != nil {
		slog.Error("failed to init config", "err", err)
		panic(fmt.Sprintf("failed to init config: %v", err))
	}

	// initialize logger with config
	config := static.GetDifySandboxGlobalConfigurations()
	if config.LogPath != "" {
		err = log.Init(config.LogPath)
		if err != nil {
			slog.Error("failed to initialize logger with config", "err", err)
			panic(fmt.Sprintf("failed to initialize logger with config: %v", err))
		}
	}

	slog.Info("config init success")

	err = static.SetupRunnerDependencies()
	if err != nil {
		slog.Error("failed to setup runner dependencies", "err", err)
	}
	slog.Info("runner dependencies init success")
}

func initServer() {
	config := static.GetDifySandboxGlobalConfigurations()
	if !config.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize OpenTelemetry by config. If disabled or init fails, run without tracing.
	cfgAll := static.GetDifySandboxGlobalConfigurations()
	if cfgAll.Otel.Enable {
		if shutdown, err := telemetry.Init(context.Background(), "dify-sandbox", cfgAll); err != nil {
			slog.Warn("opentelemetry init failed", "err", err)
		} else if shutdown != nil {
			_ = shutdown
		}
	} else {
		slog.Info("opentelemetry disabled via config; set app.otel.enable_otel=true to enable")
	}

	r := gin.New()
	if cfgAll.Otel.Enable {
		// OTel middleware should be first to ensure upstream trace id is extracted.
		r.Use(otelgin.Middleware("dify-sandbox"))
		// Attach identity and annotate tenant.id on the HTTP span.
		r.Use(middleware.Identity())
		// Expose Trace-Id header for easier troubleshooting.
		r.Use(middleware.TraceIDHeader())
	}
	if gin.Mode() == gin.DebugMode {
		r.Use(gin.Logger())
	}
	r.Use(gin.Recovery())

	controller.Setup(r)

	r.Run(fmt.Sprintf(":%d", config.App.Port))
}

func initDependencies() {
	slog.Info("installing python dependencies")
	dependencies := static.GetRunnerDependencies()
	err := python.InstallDependencies(dependencies.PythonRequirements)
	if err != nil {
		slog.Error("failed to install python dependencies", "err", err)
		panic(fmt.Sprintf("failed to install python dependencies: %v", err))
	}
	slog.Info("python dependencies installed")

	slog.Info("initializing python dependencies sandbox")
	err = python.PreparePythonDependenciesEnv()
	if err != nil {
		slog.Error("failed to initialize python dependencies sandbox", "err", err)
		panic(fmt.Sprintf("failed to initialize python dependencies sandbox: %v", err))
	}
	slog.Info("python dependencies sandbox initialized")

	// start a ticker to update python dependencies to keep the sandbox up-to-date
	go func() {
		updateInterval := static.GetDifySandboxGlobalConfigurations().PythonDepsUpdateInterval
		tickerDuration, err := time.ParseDuration(updateInterval)
		if err != nil {
			slog.Error("failed to parse python dependencies update interval, skip periodic updates", "err", err)
			return
		}
		ticker := time.NewTicker(tickerDuration)
		defer ticker.Stop()
		for range ticker.C {
			if err := updatePythonDependencies(dependencies); err != nil {
				slog.Error("Failed to update Python dependencies", "err", err)
			}
		}
	}()
}

func updatePythonDependencies(dependencies static.RunnerDependencies) error {
	slog.Info("Updating Python dependencies")
	if err := python.InstallDependencies(dependencies.PythonRequirements); err != nil {
		slog.Error("Failed to install Python dependencies", "err", err)
		return err
	}
	if err := python.PreparePythonDependenciesEnv(); err != nil {
		slog.Error("Failed to prepare Python dependencies environment", "err", err)
		return err
	}
	slog.Info("Python dependencies updated successfully")
	return nil
}

func Run() {
	// init config
	initConfig()
	// init dependencies, it will cost some times
	go initDependencies()

	initServer()
}
