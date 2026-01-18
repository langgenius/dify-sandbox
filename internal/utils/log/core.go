package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	defaultLogPath = "./logs"
)

// global configuration
var (
	mainLogger        *slog.Logger
	configuredLogPath string
	showLog           bool = true
	mu                sync.RWMutex
)

type LoggerConfig struct {
	Filename   string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

// InitFromConfig initializes the logger with configuration
func InitFromConfig(logPath string) error {
	mu.Lock()
	defer mu.Unlock()

	configuredLogPath = logPath
	return reinitializeLogger()
}

func reinitializeLogger() error {
	config := LoggerConfig{
		Filename:   getLogPath() + "/app.log",
		MaxSize:    100,
		MaxBackups: 30,
		MaxAge:     30,
		Compress:   true,
	}

	return initLoggerWithConfig(config, true)
}

func getLogPath() string {
	if configuredLogPath != "" {
		return configuredLogPath
	}
	return defaultLogPath
}

func initLoggerWithConfig(config LoggerConfig, showStdout bool) error {
	lumberjackLogger := &lumberjack.Logger{
		Filename:   config.Filename,
		MaxSize:    config.MaxSize,
		MaxBackups: config.MaxBackups,
		MaxAge:     config.MaxAge,
		Compress:   config.Compress,
		LocalTime:  true,
	}

	var writer io.Writer = lumberjackLogger
	if showStdout {
		writer = io.MultiWriter(lumberjackLogger, os.Stdout)
	}

	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{
		AddSource: false,
		Level:     slog.LevelDebug,
	})

	mainLogger = slog.New(handler)
	return nil
}

func init() {
	if err := initlog(); err != nil {
		panic(err)
	}
}

func initlog() error {
	config := LoggerConfig{
		Filename:   getLogPath() + "/app.log",
		MaxSize:    100,
		MaxBackups: 30,
		MaxAge:     30,
		Compress:   true,
	}

	return initLoggerWithConfig(config, true)
}

func logMsg(level slog.Level, format string, stdout bool, v ...interface{}) {
	mu.RLock()
	logger := mainLogger
	mu.RUnlock()

	if logger == nil {
		if err := initlog(); err != nil {
			panic(err)
		}
		logger = mainLogger
	}

	msg := fmt.Sprintf(format, v...)

	switch level {
	case slog.LevelDebug:
		logger.Log(context.Background(), slog.LevelDebug, msg)
	case slog.LevelInfo:
		logger.Log(context.Background(), slog.LevelInfo, msg)
	case slog.LevelWarn:
		logger.Log(context.Background(), slog.LevelWarn, msg)
	case slog.LevelError:
		logger.Log(context.Background(), slog.LevelError, msg)
	}

	// print to stdout if requested
	if stdout && showLog {
		switch level {
		case slog.LevelDebug:
			fmt.Println("[DEBUG]" + msg)
		case slog.LevelInfo:
			fmt.Println("[INFO]" + msg)
		case slog.LevelWarn:
			fmt.Println("[WARN]" + msg)
		case slog.LevelError:
			fmt.Println("[ERROR]" + msg)
		}
	}
}

func Debug(format string, v ...interface{}) {
	logMsg(slog.LevelDebug, format, true, v...)
}

func Info(format string, v ...interface{}) {
	logMsg(slog.LevelInfo, format, true, v...)
}

func Warn(format string, v ...interface{}) {
	logMsg(slog.LevelWarn, format, true, v...)
}

func Error(format string, v ...interface{}) {
	logMsg(slog.LevelError, format, true, v...)
}

func Panic(format string, v ...interface{}) {
	logMsg(slog.LevelError, format, true, v...)
}
