package server

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/controller"
	"github.com/langgenius/dify-sandbox/internal/core/runner/python"
	"github.com/langgenius/dify-sandbox/internal/static"
	"github.com/langgenius/dify-sandbox/internal/utils/log"
)

func initConfig() {
	// auto migrate database
	err := static.InitConfig("conf/config.yaml")
	if err != nil {
		log.Panic("failed to init config: %v", err)
	}
	log.Info("config init success")

	err = static.SetupRunnerDependencies()
	if err != nil {
		log.Error("failed to setup runner dependencies: %v", err)
	}
	log.Info("runner dependencies init success")
}

func initServer() {
	config := static.GetDifySandboxGlobalConfigurations()
	if !config.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	controller.Setup(r)

	r.Run(fmt.Sprintf(":%d", config.App.Port))
}

func initDependencies() {
	log.Info("installing python dependencies...")
	dependenices := static.GetRunnerDependencies()
	err := python.InstallDependencies(dependenices.PythonRequirements)
	if err != nil {
		log.Panic("failed to install python dependencies: %v", err)
	}
	log.Info("python dependencies installed")

	log.Info("initializing python dependencies sandbox...")
	err = python.PreparePythonDependenciesEnv()
	if err != nil {
		log.Panic("failed to initialize python dependencies sandbox: %v", err)
	}
	log.Info("python dependencies sandbox initialized")

	// start a ticker to update python dependencies every 30 minutes to keep the sandbox up-to-date
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		for range ticker.C {
			log.Info("updating python dependencies...")
			err := python.InstallDependencies(dependenices.PythonRequirements)
			if err != nil {
				log.Error("failed to update python dependencies: %v", err)
			}
			log.Info("python dependencies updated")
		}
	}()
}

func Run() {
	initConfig()
	initDependencies()
	initServer()
}
