package server

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/langgenius/dify-sandbox/internal/controller"
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
}

func initServer() {
	config := static.GetCoshubGlobalConfigurations()
	if !config.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	controller.Setup(r)
	controller.InitSandBoxQueue()

	r.Run(fmt.Sprintf(":%d", config.App.Port))
}

func Run() {
	initConfig()
	initServer()
}
